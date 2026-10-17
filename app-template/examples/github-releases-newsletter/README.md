# Super Durable Newsletter

Visitors subscribe through the application's web page. Each normalized email address is retained once in `SubscriptionRegistryFlow`.

An authorized administrator publishes a newsletter by posting exactly `publish newsletter` as a new top-level message in the configured Slack channel. The Slack message identity starts one `NewsletterCampaignFlow`; duplicate delivery of the same message does not start another campaign. The campaign freezes its subscriber list and seven-day window at the start, reads every available release page from `superdurable/dex` and `superdurable/dex-connectors-library`, excludes drafts and releases outside that window, and asks the configured OpenAI model to summarize the releases with links. If there are no releases, it sends a clear no-releases notice. It sends one Gmail message per subscriber and records sent, failed, or uncertain delivery outcomes. An uncertain Gmail response is not retried automatically.

## Studio configuration

Configure these four connections in Studio before running the application:

- `newsletter-slack`: authorize the Slack app, select the publishing channel and the administrator members permitted to publish. The app must be joined to that channel.
- `newsletter-github`: authorize GitHub access for release listing.
- `newsletter-openai`: configure the OpenAI provider for report generation.
- `newsletter-gmail`: authorize the Google account that sends newsletter email.

Studio provides Flow runs and their display. The application also exposes `GET /api/newsletters/{campaignId}` for a campaign's report and aggregate delivery status. The campaign ID derives from the Slack team, channel and message timestamp. The subscriber page uses `POST /api/subscriptions` and requires no login.

## Source verification

`make check-fdg-v2` validates the Flow definitions. `make test-app RUN='TestSubscriptionValidationAndDeduplication|TestNewsletter'` runs the HTTP API tests against the test Dex engine with connector mocks. These tests verify application source and do not establish that the configured Slack, GitHub, OpenAI, or Gmail accounts work. Verify those integrations in configured Preview before Live use.
