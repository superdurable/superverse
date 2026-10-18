# API, Studio and infrastructure

## AttachmentService

A new service in `api/superverse/v2/superverse.proto`. `AgentService` has nine RPCs and
`ProjectService` ten, so the attachment RPCs get a service of their own
(decision 0011: at most ten RPCs of one domain per
service). Every request carries `CallerContext caller = 100`, which only the Studio BFF sets.
No request has a `bytes`, map, `Struct` or `Timestamp` field, which the BFF's decoder refuses
(`web/studio/lib/server/rpc/request-decoder.ts:26,155-173`); times are strings, as in
`AgentMessage`.

| RPC | Who | What |
|---|---|---|
| `CreateAttachmentUpload` | A TeamSpace member or the platform admin | Starts the attachment's Flow, waits for its registration and returns the attachment and, while it awaits the upload, the presigned PUT ([upload](upload-and-preparation.md#upload)) |
| `CompleteAttachmentUpload` | The uploader | Ends the upload wait, then waits at most 20 seconds for READY or FAILED and returns the attachment; repeatable |
| `DeleteAttachment` | The uploader, or the platform admin | Sends the deletion command; returns the attachment |
| `GetAttachmentDownloads` | Anyone who may read the Project's conversation | Presigned GETs, valid for 5 minutes, of at most 20 attachments, inline for image thumbnails or as a download with the file name. A URL names the Project's copy, which its manifest under `<projectID>/attachments/<id>/` records and only the Project's Session writes, so the manifest is the authority that the Project holds the attachment; an attachment the Session has not shown yet, or that expired before it was rendered, has none |

```proto
service AttachmentService {
  rpc CreateAttachmentUpload(CreateAttachmentUploadRequest) returns (CreateAttachmentUploadResponse);
  rpc CompleteAttachmentUpload(CompleteAttachmentUploadRequest) returns (CompleteAttachmentUploadResponse);
  rpc DeleteAttachment(DeleteAttachmentRequest) returns (DeleteAttachmentResponse);
  rpc GetAttachmentDownloads(GetAttachmentDownloadsRequest) returns (GetAttachmentDownloadsResponse);
}

message CreateAttachmentUploadRequest {
  string team_space_id = 1;
  // Studio's idempotency key of this file; a retry repeats it.
  string request_id = 2;
  string file_name = 3;
  string media_type = 4;
  int64 byte_size = 5;
  // Lowercase hex SHA-256 of the bytes the browser will PUT.
  string sha256 = 6;
  CallerContext caller = 100;
}

message CreateAttachmentUploadResponse {
  Attachment attachment = 1;
  // Present while the attachment awaits its upload.
  AttachmentUploadTarget upload = 2;
}

message AttachmentUploadTarget {
  string url = 1;
  repeated UploadHeader headers = 2;
  string expires_at = 3;
}

message UploadHeader {
  string name = 1;
  string value = 2;
}

message CompleteAttachmentUploadRequest {
  string team_space_id = 1;
  string attachment_id = 2;
  CallerContext caller = 100;
}

message DeleteAttachmentRequest {
  string team_space_id = 1;
  string attachment_id = 2;
  // The command's identity; a retry repeats it.
  string request_id = 3;
  CallerContext caller = 100;
}

message GetAttachmentDownloadsRequest {
  string project_id = 1;
  repeated string attachment_ids = 2;
  AttachmentDisposition disposition = 3;
  CallerContext caller = 100;
}

message Attachment {
  string attachment_id = 1;
  string team_space_id = 2;
  string file_name = 3;
  string media_type = 4;
  AttachmentKind kind = 5;
  int64 byte_size = 6;
  AttachmentStatus status = 7;
  // Set once READY: image width and height, document sections and truncation, JSON format,
  // node count and redaction count.
  AttachmentSummary summary = 8;
  // Set for FAILED: the code of the closed set and a safe sentence.
  AttachmentFailure failure = 9;
  string expires_at = 10;
}
```

- `CompleteAttachmentUploadResponse`, `DeleteAttachmentResponse` carry `Attachment attachment = 1`;
  `GetAttachmentDownloadsResponse` carries `repeated AttachmentDownload downloads = 1` with
  `attachment_id`, `url` and `expires_at`, in request order, one per attachment that has one.
  `AttachmentSummary` holds `width`, `height`, `sections`, `shown_sections`, `truncated`, `pages`,
  `json_format`, `nodes` and `redactions`; `AttachmentFailure` the `code` and its sentence.
- New enums, prefixed as the proto rules require: `AttachmentStatus`
  (`ATTACHMENT_STATUS_REGISTERING` ... `ATTACHMENT_STATUS_FAILED`, the Flow's statuses),
  `AttachmentKind` (`ATTACHMENT_KIND_IMAGE`, `_DOCUMENT`, `_JSON`) and `AttachmentDisposition`
  (`ATTACHMENT_DISPOSITION_INLINE`, `_DOWNLOAD`). The Go closed sets map to them with
  `boundary.Enum`, which `make check-proto-enums` checks. Failure codes are strings of the
  closed set declared once in the attachments package.
- `scripts/check-proto-services.py`'s `ROUTES` gains `AttachmentService` as an `/api/rpc` route,
  the BFF's `BROWSER_SERVICES` and the `BrowserService` type gain it
  (`web/studio/lib/server/rpc/rpc-handler.ts`, `web/studio/lib/shared/rpc-methods.ts`), and the
  Studio mock's `SERVICES` gains its handlers, which `mock/backend/agent-service.ts` holds because
  `mock/backend` has its ten files.
- The handlers live in a new `internal/dexai/api/attachments` package: `internal/dexai/api`
  already has ten files, and has four sub-packages.

## Changed messages

| Message | Field | Rule |
|---|---|---|
| `SendMessageRequest` | `repeated string attachment_ids = 5` | At most `attachments.maxPerMessage` (10) distinct IDs; `text` may be blank when there is at least one |
| `CreateProjectRequest` | `repeated string initial_attachment_ids = 12` | Only with `message_id`; `initial_message` may then be blank; at most `attachments.maxPerMessage` |
| `AgentMessage` | `repeated AgentAttachment attachments = 16` | Set on user messages: one per reference block |
| `AgentUserMessage` (the `value` of `AgentPendingUserMessage`) | `repeated AgentAttachment attachments = 3` | Queued and steered messages |
| new `AgentAttachment` | `attachment_id`, `file_name`, `media_type`, `AttachmentKind kind`, `byte_size`, `width`, `height`, `json_format`, `redactions` | No content, no object key, no URL; `conversation.AttachmentSummary` is its source, for history entries and for pending messages alike |

No field is reserved or renumbered (pre-launch rule).

## Authorization

| RPC | Check |
|---|---|
| `CreateAttachmentUpload` | The caller's TeamSpace role from the read model (`memberdao.TeamSpaceRole`, as `admitCreator` does, `internal/dexai/api/project.go:150-164`), or the platform admin; `TeamSpaceFlow`'s reservation confirms membership by exact load |
| `CompleteAttachmentUpload`, `DeleteAttachment` | The caller is the attachment's uploader, or the platform admin for deletion, which the Flow's RPC checks against `UploaderAccountID` under its status lock |
| `SendMessage` | The existing Build authorization of the Project (`AuthorizeSession`), then for each attachment: READY, uploaded by the caller, of the Project's TeamSpace |
| `CreateProject` | The existing creator admission, then the same attachment checks against the request's TeamSpace |
| `GetAttachmentDownloads` | The caller may read the Project's conversation (Build, the Session read authorization); each ID has the Project's manifest |

Roles are memberships the server checks; nothing the browser claims grants access.

## Admission at send

`AttachmentFlowService` (`internal/dexai/agents/attachments/attachment_flow_service.go`) owns the
coordination that spans `AttachmentFlow` and `ProjectFlow`. The API's `SendMessage`
(`internal/dexai/api/agent.go`) and `CreateProject` (`project.go`) handlers use it; no Flow does.
The handler first checks the IDs (valid, distinct, at most `attachments.maxPerMessage`, which
the configuration bounds by the Session's ten) and the text (blank only with attachments), and,
for `SendMessage`, authorizes the caller's Session (`AuthorizeSession`, which also names the
Project's TeamSpace) before it reads any attachment.

1. **Read**: at most ten `GetAttachmentState` retained reads of
   `attachment-<the Project's TeamSpace>-<id>`, four at a time. Another person's upload is
   refused before its status is looked at. Each attachment must be the caller's own upload and
   READY; otherwise the request is refused and nothing is reserved. The refusals
   (`AdmissionRefusal`, whose status message starts with the code) are `attachment_not_found`
   (NOT_FOUND: the TeamSpace holds no such attachment), `attachment_not_yours`
   (PERMISSION_DENIED), `attachment_not_ready` (FAILED_PRECONDITION: still uploading or
   preparing, or FAILED) and `attachment_expired` (FAILED_PRECONDITION: EXPIRED, DELETED or
   being deleted). `SendMessage` marks each as AGENT_COMMAND_NOT_ADMITTED.
2. **Reserve**: one `ProjectFlow.ReserveProjectAttachments` call with the message ID, a new
   admission ID for this call and the attachments ([ProjectFlow](conversation.md#projectflow-changes));
   the admission holds them, and one the Project counts already is held again, uncounted. Its
   RESOURCE_EXHAUSTED is
   `attachment_quota_exceeded`, and a Project that is not READY is FAILED_PRECONDITION, both not
   admitted.
3. **Send**: `SendPendingMessage` with `Value.Attachments`, the staged references built from the
   reads ([Session](conversation.md#channels-and-channelmaps)). While a local agent executes the
   Session it refuses them (`attachments_need_platform_agent`, FAILED_PRECONDITION, not admitted).
4. **Refusal**: after a definite refusal of the send (a question waits, the executor is remote,
   the Session is closed or its engine execution stopped), `ReleaseProjectAttachments` for that
   call's admission, which frees only what no other admission holds: an earlier send of the same
   message ID, which the Session may have queued, or another message that carries the same file
   keeps it counted. A failed release is logged with the Project and message IDs, and the refusal
   stands. After an uncertain result nothing is released: the holds stay, and the attachment is
   counted once whatever retries follow.

`CreateProject` runs step 1 only, after the creator's TeamSpace admission; `ProjectFlow` counts
the initial attachments when it starts ([CreateProject](conversation.md#createproject)).

## Configuration

New keys in `cmd/superverse-server/config`, each with its default constant, its range in
`Validate` and its row in the [configuration reference](../../../cmd/superverse-server/config/README.md):

| Key | Default | Range or rule |
|---|---|---|
| `attachments.maxPerMessage` | `10` | 1 to 10, the Session's bound (`conversation.MaximumMessageAttachments`) |
| `attachments.imageLongEdge` | `2000` | 256 to 2000 (the many-image rule caps it) |
| `attachments.imageMaxBytes` | `1MiB` | 64 KiB to 4 MiB |
| `attachments.documentMaxBytes` | `20MiB` | 1 MiB to 30 MiB, below the 32 MiB object limit |
| `attachments.jsonMaxBytes` | `4MiB` | 64 KiB to 4 MiB, the workspace file write limit |
| `attachments.messageInlineText` | `120KiB` | 16 KiB to 256 KiB |
| `attachments.segmentBytes` | `20MiB` | 1 MiB to 24 MiB, below the 32 MB request limit with room for the context |
| `attachments.segmentImages` | `60` | 1 to 90, below the 100 images of 200k-context models |
| `attachments.uploadWait` | `1h` | 1 m to 24 h; a test profile shortens it |
| `attachments.stagingRetention` | `24h` | 1 h to 72 h; a test profile shortens it |
| `attachments.teamSpaceStagedFiles` | `200` | 10 to 2,000 |
| `attachments.teamSpaceStagedBytes` | `2GiB` | 100 MiB to 20 GiB |
| `attachments.projectLifetimeFiles` | `1000` | 10 to 10,000 |
| `attachments.projectLifetimeBytes` | `2GiB` | 100 MiB to 20 GiB |
| `attachments.renditionCacheBytes` | `256MiB` | 16 MiB to 1 GiB, per server replica |
| `storage.browserEndpoint` (shared with KNOW) | none (the bucket's regional S3 endpoint) | An absolute URL; required when `storage.s3Endpoint` is set; `http` only in the local profiles |

`local-kind.yaml` sets `storage.browserEndpoint: http://127.0.0.1:29000`; `native.yaml` sets the
default instance's MinIO port, and the Native launcher's instance layer sets each instance's
(`infra-as-code/scripts/native/lane.py:176`). `make check-config` validates the profiles with the
real loader. No new secret: presigning uses the server's storage credentials.

## Studio

Studio normalizes images to the default limits of the configuration (2000 px, 1 MiB) and checks the
other kinds against theirs before an upload; no RPC tells it the configured values, so a server
configured below them refuses a larger file when it is declared (`INVALID_ARGUMENT`) or read
(`too_large`), and the chip says so.

### Composer

`web/studio/components/session/composer/Composer.tsx` takes the composer's attachment tray
(`attachments`), which replaces the "not yet" Attach placeholder; the voice input keeps its
placeholder.

- **Adding files**: Attach (+) opens a file picker (several files; the extensions and media types
  of [the formats table](upload-and-preparation.md#kinds-formats-and-limits)); files dropped on the
  chat column (`AttachmentDropZone`) and images pasted into the text are added the same way. An
  eleventh file is refused with "Up to 10 files per message"; a file of another type fails at once,
  without an upload.
- **Chips** sit above the text, one per file: a kind icon (or the image's thumbnail from a local
  object URL), the name, the size and a progress ring through preparing (an image's
  [normalization](upload-and-preparation.md#images)), hashing (WebCrypto SHA-256), uploading
  (percent, the presigned PUT through `XMLHttpRequest` with exactly the signed headers) and reading
  (`CompleteAttachmentUpload`'s bounded wait, asked again while the server answers VERIFYING or
  PREPARING), then ready. × removes a chip: it aborts the PUT and the waits and deletes the upload
  (`DeleteAttachment`); a file removed while it is being declared is deleted once its ID is known.
- **What a chip says when it is ready**: an image its dimensions; a document its sections ("23
  sections, the first 7 shown"); JSON what was found ("n8n workflow · 12 nodes · 3 keys removed",
  "JSON · 2 keys removed").
- **A failed chip** says why in business language: the sentence the server's
  `AttachmentFailure.message` carries, which the attachments package declares beside each code
  (the table below), so Studio repeats no closed set of the server. Studio's own refusals before an
  upload say the same for a file of another type or over its kind's limit. Storage that refuses an
  expired presigned URL (HTTP 403) gets one fresh URL: the same request ID declares the file again,
  which signs a new PUT while the attachment awaits its bytes.

| Code | Sentence |
|---|---|
| `no_text` | This PDF has no selectable text. Attach a PDF with text, or an image of the page. |
| `encrypted` | This PDF is password-protected. |
| `unsupported_content` | This file is not a valid PNG, JPEG, WebP, GIF, Markdown, text, Word, PDF or JSON file. |
| `too_large` | This file is larger than the limit for its type. |
| `invalid_json` | This JSON could not be read: it is not valid or is nested too deeply. |
| `attachment_quota_exceeded` | Your team has too many files waiting to be sent. Send or remove some, or try again tomorrow. |
| `upload_missing`, `upload_mismatch`, `extraction_failed`, `preparation_failed` | The file could not be read. Try attaching it again. |

- **Send** waits until every chip is ready, and allows blank text when there is at least one. A
  chip that cannot be sent (failed, expired, or any chip while a local agent runs the Session)
  holds the message until it is removed. The composer sends through the tray (`AttachmentTray.send`):
  while `SendMessage` runs, the chips it carries cannot be removed (their × is disabled and
  Delete or Backspace does nothing), and a file dropped or pasted meanwhile becomes a chip for the
  next message. Once the send resolves, the message owns its attachments and the tray forgets
  exactly those chips without deleting them; a refused send keeps them and asks
  `CompleteAttachmentUpload` again where each stands, so one that expired or was removed shows
  it. A ready chip keeps its attachment's `expires_at` and turns "Expired. Attach it again." when
  it passes, while the page stays open. Edit on a queued message brings its files back as chips,
  which `CompleteAttachmentUpload` checks again.
- **Remembered chips**: once a file is declared, its chip (attachment ID, request ID, name, media
  type, size and digest, never the bytes) is kept in session storage under its composer's scope,
  the Project or `start:<TeamSpace>`, so a reload shows it at once and asks
  `CompleteAttachmentUpload` where it stands; an expired one says "Expired. Attach it again."
- **Disclosure**: at the first attach in a browser, one line under the chips: "Files are read by
  the AI model to understand your request." It is remembered per browser in local storage; when
  that storage cannot be read, the line simply shows again.
- **Local agent**: while the Session runs remotely, Attach is disabled with the tooltip
  "Attachments work with the Superverse agent", and a drop or paste shows the same note.
- **Mock model**: the mock composer carries the same tray, so a scenario's message input sends the
  attached files too, also when it steers a running turn (the steered message carries them).

### Start page

`components/project/ProjectStartForm.tsx` uses the same tray on the large composer (and on the mock
composer), uploading into the TeamSpace the Project will belong to; the local agent disables it as
above.

- `PendingProjectCreation` (`web/studio/lib/project/project-intents.ts`) gains
  `attachments: [{attachmentId, uploadRequestId, name, mediaType, size, sha256}]`, which
  `readPendingProjectCreation` validates like its other fields (one to ten distinct entries, only
  with a first message, never with an example). Bytes are never stored in `sessionStorage`.
- Build is enabled only when every attachment is ready, and the description may then be blank.
  Every Build stores a new `idempotencyKey`, so a creation whose files changed is a new request.
  Open example refuses while files are attached: an example starts without a message.
- The creation page sends the IDs as `initial_attachment_ids` with the first message's
  `message_id`; once the Project is created, the start page's remembered chips are forgotten.
  After "Start over" they come back from session storage and are checked again.

### History

- User bubbles (`components/agent/conversation/ConversationMessage.tsx`) show a chip per
  attachment with its name, kind (for JSON its format and the keys removed) and size, and a
  thumbnail for an image; a message with files and no text shows only its chips. Queued and steered
  messages show the same chips in one line, without thumbnails.
- The shared Agent UI stays transport-free: `components/agent/attachments` declares the provider
  and the Session page injects `projectAttachmentLinks` (`lib/session/attachments`), which calls
  `GetAttachmentDownloads` (inline for thumbnails, a download with the file name for a click),
  batches the requests of one moment, reuses a link until 30 seconds before it expires, and asks
  again a few seconds after an attachment had none. A thumbnail of a message the Session has not
  shown the model yet has no copy, so the chip asks again for about two minutes. A click assigns the
  download URL, whose `Content-Disposition: attachment` keeps the page where it is; a JSON attachment
  downloads as its redacted copy.
- An attachment that the model saw as expired or as a placeholder shows its chip as usual; the
  agent's reply says what it could not read.

### Accessibility, cancellation and mock

- Every chip is a button with a label ("order-process.png, image, 412 KB, ready"); the tray's
  state ("2 files attached, 1 ready, 1 in progress") is announced through a polite `aria-live`
  region, and a refusal (too many files, attaching unavailable) through a `status` element; both
  stay mounted while the tray is empty, so the first chip and a refusal on an empty tray are
  announced. Delete or Backspace on a focused chip removes it, and the focus moves to the next
  chip, else the previous one, else the composer's text; the tray is reachable by keyboard.
- Each chip has its own state machine keyed by its request ID; responses for a removed chip are
  ignored, and unmounting aborts the PUT and the waits (nothing is deleted then: the remembered
  chips come back).
- The Studio mock (`web/studio/mock/store/attachments`) stages uploads in memory: the upload target
  is the same-origin route `app/api/mock-attachments/[attachmentId]`, which checks the signed type,
  length and digest; `CompleteAttachmentUpload` prepares the bytes at once (the content must sniff as
  its type; image dimensions, Markdown headings as sections, n8n detection and redaction), and the
  route serves downloads of the attachments a Project's messages carry. A file whose content is not
  its type fails as `unsupported_content`.

### Source tree

| Directory | Holds |
|---|---|
| `lib/api/attachment.ts`, `lib/api/transport/presigned-upload.ts` | The `AttachmentService` client (`CreateAttachmentUpload` retried on `UNAVAILABLE` with its request ID); the PUT with progress through `XMLHttpRequest` |
| `lib/session/attachments/` (new; `lib/session` has ten files) | The formats and limits, image normalization and hashing, the upload pipeline, the chip state machine, the tray hook and the download links, and the generated-contract mapping |
| `components/session/composer/attachments/` (new) | The tray of chips, Attach (+) and the drop zone |
| `components/agent/attachments/` (new; `components/agent/conversation` has nine files) | The links provider and the message chips |
| `mock/store/attachments/`, `app/api/mock-attachments/[attachmentId]/route.ts` | The mock's staging and its PUT and GET route |
| `tests/e2e/support/attachments/` | The `create-project` case's start-page files and their checks |

Every new directory has its generated README (`make generate-studio-readmes`); files stay under 400
lines.

## Infrastructure

| Where | Change |
|---|---|
| AWS | One CORS rule on each environment's artifact bucket (`aws_s3_bucket_cors_configuration` beside `infra-as-code/terraform/cloud/artifacts.tf`): origin the environment's Studio origin; methods `PUT`, `GET`, `HEAD`; the signed headers (`content-type`, `x-amz-checksum-sha256`, `x-amz-meta-sha256`, `if-none-match`, the KMS headers); `ETag` exposed. A bucket takes up to 100 CORS rules (AWS S3 user guide, checked 2026-10-17); this uses one. The server role already reads, writes and deletes in the bucket with its KMS key; no IAM change |
| Server Pod | The memory limit grows by 1 GiB for the extractor child, from 2 GiB (`infra-as-code/kubernetes/charts/superverse-platform/values.yaml:19-21`), once for attachments and KNOW |
| Local Kind | `storage.browserEndpoint` in `cmd/superverse-server/config/local-kind.yaml`; the existing `kind ports` MinIO forward serves it (`infra-as-code/scripts/kind/cluster.py:50`); no chart route |
| Local Native | `storage.browserEndpoint` in `native.yaml` and the launcher's instance layer (`infra-as-code/scripts/native/lane.py:176`) |
| Platform Dex | Nothing: no Attribute Store entry, since nothing is synced |
| Deployment notes | The CORS rule is applied by the standard Terraform release; no manual step |

## Packages

`internal/` keeps its two entries. Levels follow decision 0010
and `scripts/check-package-layers.py`'s `LEVELS`, which gains the new business packages.

| Package | Holds | Level |
|---|---|---|
| `internal/comm/fileextract` (new) | The image, document and JSON readers, the redaction, the child entry point and its limits; the parent side with the per-replica slot | shared root |
| `internal/comm/platform/objectstore` (changed) | Presigned PUT and GET, a head read with the checksum, server-side copy; `S3API` (`objects.go:39-44`) gains `HeadObject` and `CopyObject` | shared root |
| `internal/comm/flowid` (changed) | `AttachmentFlowID`, `AttachmentCreationID` | shared root |
| `internal/dexai/agents/attachments` (new) | `AttachmentFlow`, its client and bootstrap, preparation and renditions, `RenditionStore`, `AttachmentFlowService` | 3 (imports `teamspaces`, 1, and `projects`, 2) |
| `internal/dexai/api/attachments` (new) | `AttachmentService` handlers | 10 |
| Changed | `tenancy/teamspaces` (1), `projects` (2), `session/conversation` (2), `sandbox/workspace/git` (3), `session/sandboxtools` (5), `session/agentloop` (6), `session` (7), `session/modelprovider/mockmodel` (7), `diagnostics` (10), `api` (11), `bootstrap` (12), `toolfailure` (1) | unchanged |

- Ports keep the import direction: the Session declares `attachmentRenditions`, `sandboxtools`
  declares its attachment-object reader, `diagnostics` takes the store, and
  `internal/dexai/bootstrap` injects `attachments.RenditionStore` into all three.
- **Package limits.** `internal/dexai/agents` gets its ninth sub-package (KNOW's `knowledge`
  would be the tenth); `internal/dexai/api` its fifth; `internal/comm` its fifth.
  `internal/comm/platform` stays at ten. The Session's packages are full, so the changes join
  files with room ([room](conversation.md#steps-rpcs-and-recovery)).
- The server binary gains the `file-extract` subcommand beside `migrate`
  (`cmd/superverse-server/main.go:34`).
