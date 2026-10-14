# Credential read endpoint

Applications read the credentials of the TeamSpace connections their scope selects through
one endpoint of the platform server. Like the [Slack event relay](slack-relay.md), it is
outside the business protocol: it is not in `api/superverse/v2/superverse.proto`, Studio
never calls it, it bypasses the Studio BFF and carries no `CallerContext`. The connector
SDK's `projectconfig` package (`sdkgo`) is its only client; its README ("Shared
connections") states the same contract from the client's side. The owning package is
[`internal/tenancy/connections`](../../internal/tenancy/connections/README.md); the design is
[connections](../architecture/teamspace/connections.md#the-read-endpoint-and-scope-tokens)
and [the current contract](../architecture/teamspace/connections.md).

## Request

```text
POST /internal/connection-credentials/read
Authorization: Bearer <scope token>
Content-Type: application/json

{"sharedConnectionId": "<TeamSpace connection ID>", "rejectedRevision": 3}
```

The JSON body is at most 4 KiB and has no unknown members:

| Member | Meaning |
| --- | --- |
| `sharedConnectionId` | Required: the TeamSpace connection the configuration entry selects (the entry's `sharedConnectionId`). |
| `rejectedRevision` | Optional, present only when a provider rejected the credential of that revision: it forces a renewal of the grant issued longest ago, unless that grant was issued within the forced-refresh floor ([Reading credentials](../architecture/teamspace/connections.md#reading-credentials)). |

## Authorization

The scope token is random, one per Project scope (Preview or Live), and the platform stores
only its SHA-256 hash. The application reads it from the secret object
(`DEX_CREDENTIAL_BROKER_TOKEN`) that its configuration document's
`credentialBroker.tokenSecretRef` names; the document's `credentialBroker` also names the
endpoint's URL (`connections.readURL`). The token gives the Project and scope, and the scope's
usages authorize the connection: a Preview token never reads a connection only Live uses. A
deleted Project's tokens are deleted, so its reads get 401.

## Response

Each of the three `status` answers is HTTP 200 with a JSON object of at most 1 MiB and
`Cache-Control: no-store`; every other outcome is an [error](#errors):

| Member | Meaning |
| --- | --- |
| `status` | `READY`, `REAUTHORIZATION_REQUIRED` or `CONNECTION_NOT_SELECTED`. |
| `credential` | `READY` only: the credential object without the host-kept fields, at most 64 KiB. |
| `revision` | `READY` only: a positive integer that changes whenever the credential changes. |
| `issuedAt` | `READY`: when the credential was issued, RFC 3339; absent for a static credential. |
| `expiresAt` | `READY`: when it expires, RFC 3339; absent for a static credential. |

- `READY`: the credential, renewed first when due.
- `REAUTHORIZATION_REQUIRED`: the connection needs a new authorization in Studio; the
  application reports `ErrReauthorizationRequired`, terminal for the call.
- `CONNECTION_NOT_SELECTED`: the scope does not use the connection (no usage row). It is
  always terminal for the application, which never follows a changed selection: the next
  Preview start reads the new configuration document, and in a frozen Live document it is a
  platform defect.
- `credential` holds every field of the credential except those the connector release lists
  as `authMethods[].hostKeptCredentials`: the refresh token and client secret of an OAuth
  connection, and the key or secret that a service account or client credentials connection
  mints from. The platform keeps those and renews the credential itself. Every other field,
  such as the minted access token, a client ID or a delegated user, is returned.
- The response carries no settings: settings come with the configuration entry.

## Errors

Errors are plain text and never contain a token, a credential or a request body.

| HTTP status | Meaning | Client behavior |
| --- | --- | --- |
| 400 | Malformed request, or no `sharedConnectionId` | Rejected |
| 401 | Missing or unknown scope token, or a deleted Project's | Reread the configuration head's token reference once, then rejected |
| 404, 405 | Another path, or a method other than POST | Rejected |
| 409 | The connection's connector release predates shared connections: its release metadata lists no `hostKeptCredentials` | Rejected; select a connection of a newer release |
| 413 | Request body over 4 KiB | Rejected |
| 429 | More than 64 reads in flight on the serving replica, or more than 8 of the token's Project scope there | Unavailable: unexpired cached credentials keep working |
| 503 | The database or the provider gave no answer within the read's budget | Unavailable: unexpired cached credentials keep working |
| 500 | Unexpected server failure, or a credential over 64 KiB | Unavailable |

Malformed, wrong-verb and oversized requests are refused before any read. Redirects are
never sent. Requests and responses are never logged.

## Renewal

A read renews the credential when its due grant needs it, as the connector's manifest
declares (refresh, client credentials or JWT bearer), under the connection's lease, so
concurrent reads renew once and the others wait for its result. A provider's terminal error
code, or scopes that no longer cover the declared ones, sets the connection to
`REAUTHORIZATION_REQUIRED` and records why on it. When the provider gives no usable answer,
the read returns the current credential while it has not expired. A renewal that the
platform has sent to a provider completes and is stored even when the client disconnects, so
a rotated token is never lost.

## Transport and limits

- The endpoint has its own listener (`connections.readListen`): port 8086 in-cluster, the
  instance's own port on Local Native, where it listens on the loopback and containers reach
  it as `host.docker.internal`. The listener serves only this endpoint; the Slack relay's
  [trigger registration](slack-relay.md#trigger-registration) is designed to share it. The
  advertised address (`connections.readURL`) is written into frozen Live configuration, so it
  is fixed per environment.
- Inside the cluster it speaks plain HTTP, like the rest of the cluster's traffic. A
  NetworkPolicy admits only the Project application Pods and the Preview Sandboxes to that
  port on Kind and hosted lanes; the scope token and the usages limit what a caller can
  read. TLS is a later option.
- A read ends within 35 seconds, waiting for another read's renewal included, below the
  listener's 40-second write timeout. At most 64 reads are in flight, and at most 8 of one
  Project scope; a read beyond either is HTTP 429. Both bounds are per server replica: with N
  replicas the endpoint admits up to N × 64 reads, and the reads of one scope that the load
  balancer spreads over several replicas count separately on each.
