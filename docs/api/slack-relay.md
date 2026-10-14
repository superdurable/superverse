# Slack event relay

[Slack event relay](../architecture/teamspace/connections.md#slack-event-relay).

The platform receives the Events API deliveries of its own Slack application and relays
each event to the applications whose `platform` Slack connections serve that Slack
workspace. Three HTTP contracts make up the relay; none is part of the business protocol,
Studio never calls them, they bypass the Studio BFF and carry no `CallerContext`:

1. [Slack events](#slack-events): Slack calls the platform.
2. [Trigger registration](#trigger-registration): an application tells the platform where a
   connection's Slack trigger receives events.
3. [Relayed events](#relayed-events): the platform calls the application.

## Slack events

```text
POST /api/slack/events
X-Slack-Request-Timestamp: <unix seconds>
X-Slack-Signature: v0=<hex HMAC-SHA256>
Content-Type: application/json
```

- Served by its own public listener and port, which serves nothing else. The platform's
  load balancer routes exactly this path to it; it never passes through Studio, so the
  handler reads the raw bytes Slack signed.
- **Every request answers 200**, at once and whatever happens to it. Slack disables the
  application's event subscription for every customer when too many deliveries fail, so the
  platform's own trouble, including a Signing Secret rotated in Slack before the
  environment or a drifting clock, must never look like a failed delivery. Each refusal
  below drops the request and logs its reason.
- The handler reads at most `connections.slackRelay.queue.maxEventBytes` of the body (1 MiB
  by default); a larger body is dropped.
- Before it parses JSON it verifies the signature: HMAC-SHA256 with the platform Slack
  application's Signing Secret over `v0:<timestamp>:<raw body>`, compared in constant time.
  A timestamp more than 5 minutes from the server's clock, or a missing or wrong signature,
  drops the request.
- `url_verification` with a valid signature answers with the request's `challenge`; without
  one it answers an empty 200.
- Every other verified event is routed and put into the receiving replica's queue; the
  handler never waits for a delivery. The routing query, like the uninstall update below,
  has a 1-second deadline, within the 3 seconds Slack allows; Slack disables the event
  subscriptions when more than 95% of deliveries within 60 minutes fail
  ([Events API](https://docs.slack.dev/apis/events-api/)). A query failure or timeout, a full
  queue or an event without a route drops the event.
- `app_uninstalled` and `tokens_revoked` are not relayed: they set the matching connections
  to `REAUTHORIZATION_REQUIRED`
  ([uninstalls](../architecture/teamspace/connections.md#uninstalls)).
- Neither this handler nor the delivery logs a payload, a header value or a signature.
  Records carry the event ID, the Slack team and enterprise IDs, the Project, the scope and
  a drop reason.

## Trigger registration

Served on the credential read endpoint's listener, beside
[the credential read](credential-read.md), with the same scope token.

```text
POST /internal/slack-triggers/register
Authorization: Bearer <scope token>
Content-Type: application/json

{"sharedConnectionId": "<TeamSpace connection ID>", "connectionName": "support"}
```

- The path is fixed; sdkgo resolves it against the origin of the configuration document's credential broker endpoint, so the document carries no new field.
- The application's Slack connector calls it every time its trigger runner starts the
  relayed receiver of a connection name: "connection C, which this scope selects as
  `connectionName`, receives relayed Slack events".
- `connectionName` is the application's connection name: 1 to 256 characters, no leading or
  trailing space, no NUL, CR or LF, the same rule as a trigger inbox key.
- **The request carries no address.** The platform derives the receiver's URL itself, from
  the scope's public route and the connection name
  ([Relayed events](#relayed-events)), so a caller can never make the platform call another
  host or port. The request decoder refuses unknown members, so a request that names a URL
  or a host is refused 400.
- A request body is at most 4 KiB.

Every outcome below is HTTP 200 with a JSON object whose `status` is one of:

| `status` | Meaning |
| --- | --- |
| `REGISTERED` | The registration is stored (inserted, or its time refreshed). |
| `CONNECTION_NOT_SELECTED` | The scope has no usage row of the connection. Terminal for the application, as for a read. |
| `NOT_RELAYED` | The connection is not a `platform` Slack connection; the connector keeps Socket Mode with its own app-level token. |

Errors:

| HTTP status | Meaning | Client behavior |
| --- | --- | --- |
| 400 | Malformed request, an unknown member or an invalid connection name | Rejected |
| 401 | Missing or unknown scope token | Reread the configuration head's token reference once, then rejected |
| 409 | The usage already has 8 registered connection names | Rejected |
| 413 | Request body too large | Rejected |
| 429, 503 | The platform is overloaded or unavailable | Retried with backoff while the runner runs |
| 500 | Unexpected server failure | Retried with backoff while the runner runs |

A registration lives on its usage row and goes with it: the Preview's freeze, "release
unused connections" and the Project deletion remove it with the usage. The application
registers again on every start, so a removed registration comes back with the next start
of an application that still selects the connection.

## Relayed events

The last path segment is the connection name in unpadded base64url (RFC 4648 §5 without `=`).

```text
POST <scope public URL>/triggers/slack/<base64url(connectionName)>
Dex-Relay-Timestamp: <unix seconds>
Dex-Relay-Signature: v1=<hex HMAC-SHA256>
Content-Type: application/json

{"event_id": "Ev0123", "event_time": 1760227200, "team_id": "T0123", "event": {...}}
```

- **Address**: the scope's existing public route, which the platform derives from the
  Project ID as it does for the Preview and Live URLs; never an address the application
  names. The application's HTTP server serves the receiver at the trigger path, as it serves
  any webhook connector's receiver, so the public route reaches it. The platform reaches the
  route the way it already probes it: on AWS through the public URL, on Local Kind through
  the `local-preview-gateway` with the route's Host header, on Local Native through the
  lane's gateway with the Host header
  ([network paths](../architecture/teamspace/infrastructure.md#network-paths)). A Project
  without a running Preview or without Live answers no 200 there, so the event is dropped.
  Public reachability is safe because the receiver accepts only a valid signature.
- **Body**: Slack's own field names and values, copied: `event_id`, `event_time`, `team_id`
  (the outer one, unchanged) and `event`, which is the byte-for-byte JSON of Slack's inner
  event object; the platform never picks or rewrites fields inside it. The outer callback's
  other members stay with the platform, among them `authorizations` (another installation's
  context) and the deprecated verification `token`, which Slack's documentation still shows
  in the callback ([Events API](https://docs.slack.dev/apis/events-api/)). This is the shape
  the Slack connector's Socket Mode payload already decodes.
- **Signature**: HMAC-SHA256 keyed with the scope's token over
  `v1:<timestamp>:<raw body>`, sent as `Dex-Relay-Signature: v1=<hex>` with the Unix-seconds
  timestamp in `Dex-Relay-Timestamp`. Both names are sdkgo's `projectconfig.RelaySignatureHeader`
  and `projectconfig.RelayTimestampHeader` (sdkgo v0.27.0), which the platform references
  instead of spelling them again. The application verifies it
  in constant time and refuses a timestamp more than 5 minutes from its clock. The token
  never travels on this path. The platform reads the token from the scope's secret object
  (`scope_credential_tokens.token_secret_ref`) and caches it in memory per scope; a receiver
  whose verification fails rereads the token once, rate-limited, as a 401 from the read
  does, so a token rotation keeps working.
- **Answer**: the receiver is an `sdkgo/webhooktrigger` endpoint. It answers 200 only after
  every accepting binding's durable inbox recorded the event, 400 for a bad signature or
  body, 413 for an oversized body and 503 while no source runs or recording fails
  ([webhooktrigger](https://github.com/superdurable/dex-connectors-library/blob/main/sdkgo/webhooktrigger/endpoint.go)).
  The platform treats anything but 200, or no answer within
  `connections.slackRelay.delivery.timeout`, as a failed delivery: the event is dropped and
  logged with the status, never retried. Redirects are not followed.

## Recent events

The relay keeps the metadata of the events it routed, so that a builder or operator can see what reached an environment.

- **Scope.** Entries are kept per Project scope (`projectID/scope`): the newest 50 of a scope, for at most 512 scopes, the least recently written scope dropped first. Every string is cut to 128 bytes, which bounds the whole to about 25 MiB.
- **Memory only.** Entries are not persisted: a restart clears them, and each replica keeps the events it received. The list reflects the replica that answers, and it carries the time the process began recording: an empty list proves only that nothing arrived since then, not that Slack sent nothing.
- **Metadata only.** An entry holds the receive time, Slack's event ID, the inner event's `type` and `subtype`, the Slack team ID, the inner `channel` (empty when Slack sends an object), the connection name, the fate, the application's HTTP status (0 without an answer) and the delivery's duration. The relay reads these three inner members with a struct that has no member for text, users, files or blocks. It never stores a body, a token or a signature.
- **Fates.**

| Fate | When |
| --- | --- |
| `RECORDED` | The application answered 200. |
| `NOT_RUNNING` | A transport error, or a gateway 502 or 504. |
| `APPLICATION_UNAVAILABLE` | The receiver answered 503: no source runs, or recording failed. |
| `REFUSED` | Any other answer but 200. |
| `TIMED_OUT` | No answer within `connections.slackRelay.delivery.timeout`. |
| `TOKEN_UNAVAILABLE` | The scope token could not be read to sign the delivery. |
| `QUEUE_FULL` | The queue had no room for the event (total or scope limit). |

  An event of an installation with no route is counted per TeamSpace connection (at most 1024 counters, memory only): the active platform connections of that Slack workspace each count it, with the time and ID of the latest. This is the common reason an environment sees nothing.

  Events dropped before routing (no route, malformed, invalid signature) have no scope and are not listed; an event over the per-event size limit is dropped without an entry.
