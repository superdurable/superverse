# Slack relay manual test

A developer's manual test of the Slack event relay with a real Slack workspace, on a Local Native instance
reached through [localtunnel](https://github.com/localtunnel/localtunnel). It is **not a lane test and not
acceptance evidence**: the relay's lane tests sign events with the lane's own test Signing Secret
(`TestSlackRelayDexIntegration`), and Slack's own deliveries are verified in the test environment with a
Slack test workspace. Nothing of this setup is committed: the Slack app's secrets, the configuration layer
and the proxy stay on the developer's machine.

## What reaches the machine

Slack must reach two paths, and nothing else of the machine may be public:

| Path | Who calls it | Local target |
| --- | --- | --- |
| `POST /api/slack/events` | Slack's servers (Event Subscriptions) | the server's Slack events listener, `connections.slackRelay.listenAddress` |
| `GET /api/connector-oauth/callback` | the developer's browser, redirected by Slack after an installation | Studio, `studio.publicOrigin` |

**Never tunnel Studio's or the server's port directly**: a public Studio exposes every account's login and the
business API. Tunnel only a local proxy that forwards these two paths and answers 404 to everything else.
The proxy forwards Slack's POST, body and `X-Slack-*` headers unchanged, to the events listener, and answers the
callback with `303` to the same path and query on the local Studio, so the browser completes the authorization
with its own Studio cookies, which belong to `localhost`:

```sh
# slack_tunnel_proxy.py <listen port> <events listener host:port> <Studio origin>
python3 slack_tunnel_proxy.py 24588 127.0.0.1:<events port> http://localhost:<Studio port>
```

```python
#!/usr/bin/env python3
"""Forwards POST /api/slack/events to the events listener, redirects GET /api/connector-oauth/callback to
the local Studio and answers 404 to everything else. Logs methods, paths and statuses only."""
import http.client, http.server, sys, urllib.parse

LISTEN, EVENTS, STUDIO = int(sys.argv[1]), sys.argv[2], sys.argv[3].rstrip("/")
EVENTS_PATH, CALLBACK_PATH = "/api/slack/events", "/api/connector-oauth/callback"
FORWARDED = ("Content-Type", "X-Slack-Request-Timestamp", "X-Slack-Signature", "User-Agent")

class Proxy(http.server.BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        sys.stderr.write("%s %s %s\n" % (self.command, urllib.parse.urlsplit(self.path).path, args[1] if len(args) > 1 else ""))

    def do_POST(self):
        target = urllib.parse.urlsplit(self.path)
        if target.path != EVENTS_PATH or target.query:
            return self.send_error(404)
        length = int(self.headers.get("Content-Length") or 0)
        if length > 1 << 20:
            return self.send_error(413)
        body = self.rfile.read(length)
        connection = http.client.HTTPConnection(EVENTS, timeout=5)
        try:
            connection.request("POST", EVENTS_PATH, body, {name: self.headers[name] for name in FORWARDED if self.headers.get(name)})
            answer = connection.getresponse()
            payload = answer.read(64 << 10)
            self.send_response(answer.status)
            self.send_header("Content-Type", answer.getheader("Content-Type") or "text/plain")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
        except OSError:
            self.send_error(502)
        finally:
            connection.close()

    def do_GET(self):
        target = urllib.parse.urlsplit(self.path)
        if target.path != CALLBACK_PATH:
            return self.send_error(404)
        self.send_response(303)
        self.send_header("Location", STUDIO + CALLBACK_PATH + ("?" + target.query if target.query else ""))
        self.send_header("Cache-Control", "no-store")
        self.send_header("Referrer-Policy", "no-referrer")
        self.send_header("Content-Length", "0")
        self.end_headers()

http.server.ThreadingHTTPServer(("127.0.0.1", LISTEN), Proxy).serve_forever()
```

Find a Native instance's ports in its run layer, `.local/superverse-v2/native-<instance>/runs/<run>/native-run.yaml`
of the main checkout: `connections.slackRelay.listenAddress` and `studio.publicOrigin`. The default instance
uses `127.0.0.1:26088` and `http://localhost:21000`; a named instance adds its offset to both.

## The tunnel

```sh
npm install -g localtunnel
TUNNEL_SUBDOMAIN=your-subdomain
lt --port 24588 --subdomain "$TUNNEL_SUBDOMAIN"   # prints https://<subdomain>.loca.lt when the name is free
```

- **The URL can change on every run.** `--subdomain` only asks for the name; when it is taken, or after the
  client reconnects, loca.lt assigns another one. In practice a restarted `lt` often loses the subdomain for a
  while; stop it and retry until it prints the requested host. After a change, update the Slack app's Redirect URL and
  Event Subscriptions Request URL, and `connections.oauthCallbackOrigin` of the private layer below, and
  restart the instance.
- **The reminder page.** loca.lt shows a "friendly reminder" page, which asks for the tunnel password, to
  requests from standard browser User-Agents, once per subdomain and visitor IP per 7 days. The password is
  the public IP address of the machine that runs `lt`: `curl ipv4.icanhazip.com`. The OAuth callback is a
  browser visit: the first time, enter the IP on that page and the callback continues to the proxy, which
  redirects to the local Studio. Opening `https://<subdomain>.loca.lt/api/connector-oauth/callback` once
  before installing the app clears the page ahead of time (the proxy answers it with a redirect to Studio,
  which refuses it without an attempt).
- **Slack's event POSTs.** The public [localtunnel server source](https://github.com/localtunnel/server) has no
  reminder logic at all; the page is part of the hosted loca.lt service, whose code is not published, so its
  behavior cannot be confirmed from source. Its operator states that the page appears only for standard
  browser User-Agent values and that a `bypass-tunnel-reminder` request header of any value skips it
  ([localtunnel](https://github.com/localtunnel/localtunnel/issues/663),
  [localtunnel](https://github.com/localtunnel/localtunnel/issues/366)). Slack's Events API requests carry
  `User-Agent: Slackbot 1.0 (+https://api.slack.com/robots)`, which is not a browser's, so they pass. Observed
  in the first manual run: the reminder page appeared once, on the browser's OAuth callback, and Slack's
  `url_verification` and event POSTs reached the proxy without it. Slack cannot be made to send the bypass
  header, so if the hosted service changes its rule and Slack's Request URL
  verification fails with a 401 or HTML answer, use a tunnel without an interstitial page instead, such as a
  Cloudflare quick tunnel to the same proxy (`cloudflared tunnel --url http://127.0.0.1:24588`), and use its
  `https://<name>.trycloudflare.com` host everywhere this page names the tunnel host.

## The Slack app

Create a separate app for local tests, such as **Superverse Local**, never the platform's production app:

1. **OAuth & Permissions**: Redirect URL `https://<tunnel host>/api/connector-oauth/callback`; bot token
   scopes `channels:history`, `groups:history`, `channels:read`, `groups:read`, `users:read`, `chat:write`; user
   token scopes `channels:history`, `groups:history` (Slack `v1.0.0`'s declared scopes, which the platform
   client must list). Leave **Token Rotation off**: once on, it cannot be turned off.
2. **Socket Mode off**: the platform relays the events over HTTP.
3. **Event Subscriptions**: on, Request URL `https://<tunnel host>/api/slack/events`, bot events
   `message.channels` and `message.groups`. Slack verifies the URL with a signed `url_verification`, which the
   relay answers only when the Signing Secret matches. Slack sends the verification when the field is
   submitted with Enter, not when it loses focus.
4. **Manage Distribution** only when installing into a workspace other than the app's own.
5. Note the Client ID; keep the Client Secret and the Signing Secret private.

## The instance

The secrets reach the Native launcher in its environment, or in the main checkout's ignored `.env`; the
launcher passes them to the server and never prints them:

| Variable | Value |
| --- | --- |
| `SUPERVERSE_SLACK_SIGNING_SECRET` | the app's Signing Secret; it replaces the lane's test secret, so the relay lane tests fail while it is set |
| `SUPERVERSE_CONNECTOR_OAUTH_CLIENT_SECRETS` | `{"slack/default": "<Client Secret>"}`: Slack has one unnamed authentication method, whose key is `default` |

A private configuration layer, outside the repository, adds the platform client and the callback origin:

```yaml
connections:
  # The redirect URI's origin; the proxy redirects the callback to the local Studio.
  oauthCallbackOrigin: https://<tunnel host>
  platformClients:
    slack/default:
      clientId: "<Client ID>"
      scopes: [channels:history, groups:history, channels:read, groups:read, users:read, chat:write]
```

```sh
./infra-as-code/sv2 native start --studio build --instance <name> --config /path/to/private-layer.yaml
```

The server refuses to start with a `slack/...` platform client and no Signing Secret. A script that reads the
secrets from another file must export them without echoing them, and must not pass them as command
arguments, which other processes can read.

Local Kind takes the same two variables from the main checkout's `.env` at `sv2 kind deploy`, and the same
layer through `sv2 kind deploy --config`; its events listener is the `kind ports` forward `127.0.0.1:28088`.

## Walk-through

1. Start the instance, the proxy and the tunnel; set the app's URLs to the tunnel host and let Slack verify
   the Request URL.
2. In Studio, create the TeamSpace's Slack connection with the platform client and authorize it; Slack
   returns through the tunnel, and the connection becomes READY with its workspace recorded.
3. Build an application whose Flow declares its Slack trigger bindings (`GetConnectorTriggerBindings` with
   Connector UI units), select the connection in the Project's Tools ▸ Setup, and configure each binding
   there (the channel, and for a reply binding the allowed repliers' Slack user IDs). When its Preview starts,
   `slack_trigger_registrations` has its row.
4. Invite the app's bot to the channel and post a message: Slack delivers it to the tunnel, the relay routes
   it by the workspace, and the application's trigger starts its Flow. A drop is a WARN record
   `slack relay dropped an event` with its reason in the server log.
5. Stop the tunnel when done, and remove the variables and the layer before running the lane tests.
