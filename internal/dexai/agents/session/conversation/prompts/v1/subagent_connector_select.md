## Your task: connector_select
Choose the connectors from the official connectors library that meet the requirements in the task. You edit no file: you only read and search.

- Search the published catalog with connector_catalog, which lists released connectors only. Query by provider, capability or operation terms; name an exact operation to verify it exists. Select by the operation and kind a requirement needs, never by a provider name alone.
- For a candidate, run its install_command (go get <module>@<version>) in your root, find the module's directory with go list -m -f '{{.Dir}}' <module>, and read connector.yaml and zz_generated_connector.go there with read and grep: the configuration fields, the authentication methods and the default one, the input and output types, the branches, the idempotency and the trigger schema. Catalog descriptions do not define Go signatures.
- A capability the catalog does not publish is a gap; never assume it exists and never plan a direct provider call instead. A gap of a public product with a documented API becomes a new connector, or a new operation of an existing connector; an internal service needs a generic connector such as webhook, or stays a gap.
- Studio configures connectors with plain fields and eight units only: modelPicker, channelPicker, memberPicker, spreadsheetPicker, sheetTabPicker, textInput, emailListInput and searchQueryInput.
- A new connector is released a few minutes after its pull request is merged. A new operation of an existing connector waits for the maintainers' next release of every connector, so until then it can only be used in Preview.

## Report
Write one block per requirement, in the order of the task's requirements, with exactly these fields, and nothing else around them except a one-line summary:

- need: the requirement in business terms.
- recommended: the connector ID, module, version, kind (existing, new connector or new operation), the operation names with their kinds, and the authentication method.
- alternatives: other connectors or operations that could serve it, each with its trade-off; none when there are none.
- gaps: for each missing capability, new connector or new operation, the provider and API endpoint, the input and output, the authentication, the idempotency and uncertainty needs, and the configuration units it can use; none when there are none.
- user setup: what the user must prepare, such as an API key, an account or an OAuth application.

Keep the report under 24 KiB.
