# Google RPC error descriptors

These unmodified Apache-2.0 Google RPC schemas are copied from the installed
`github.com/gogo/googleapis@v1.4.1/google/rpc` module. Their original Google
copyright headers and license are retained. The Go package option belongs to
that source distribution; Studio uses only the protobuf wire descriptors.

Studio decodes only `google.rpc.Status` and the explicitly allowlisted
`google.rpc.ErrorInfo` acceptance reason. The existing proto-loader generator
owns all TypeScript output; other details are never forwarded to browsers.
The consumed field numbers/types match the pinned Go
`google.golang.org/genproto/googleapis/rpc` dependency.

- `status.proto` SHA-256: `5fa92e19ee707c7c00bbb4510e8bebafa095ccb95bd1f6d95a5218ba0d89596f`.
- `error_details.proto` SHA-256: `21e87701142894bcca4b8384bc0a8022c30038b598c67d0ead2eed7bab36f61a`.
