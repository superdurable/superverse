// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, type Page, type Request, type Response } from "@playwright/test";
import { crc32, deflateSync } from "node:zlib";
import type { RpcInput } from "@/lib/shared/rpc-methods";
import { readSession, type FirstTurnChoice } from "@/tests/e2e/support/project";
import { rpcPosted, rpcResult, studioOrigin } from "@/tests/e2e/support/studio-api";

/*
 * The start page's attachments: a PNG and an n8n export join the first message, each through Studio's real
 * upload (CreateAttachmentUpload, the presigned PUT to storage, CompleteAttachmentUpload). The fixtures are
 * made here: the PNG is drawn in code and the export holds synthetic values shaped like secrets, which the
 * server's preparation must remove before the model, the workspace or a download sees the file.
 */

const IMAGE_NAME = "order-process.png";
const EXPORT_NAME = "lead-intake.json";
/** The marker the server's JSON preparation writes in place of each secret value. */
const REDACTION_MARKER = "‹redacted:superverse›";
const SYNTHETIC_VALUES = ["EXAMPLEQUERYKEY0001NOTREAL", "EXAMPLEBEARER0001NOTREAL", "EXAMPLEBODYKEY0001NOTREAL"];

function chunk(type: string, data: Buffer): Buffer {
  const length = Buffer.alloc(4);
  length.writeUInt32BE(data.length);
  const body = Buffer.concat([Buffer.from(type, "latin1"), data]);
  const checksum = Buffer.alloc(4);
  checksum.writeUInt32BE(crc32(body));
  return Buffer.concat([length, body, checksum]);
}

/** A small RGB PNG of three boxes in a row, like a process diagram. */
function processDiagram(width = 96, height = 32): Buffer {
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0);
  header.writeUInt32BE(height, 4);
  header.set([8, 2, 0, 0, 0], 8);
  const rows = Array.from({ length: height }, (_, y) => {
    const row = Buffer.alloc(1 + width * 3, 0xff);
    for (let x = 0; x < width; x++) {
      const inBox = x % 32 > 3 && x % 32 < 28 && y > 6 && y < 26;
      if (inBox) row.set([0x2d, 0x6c, 0xdf], 1 + x * 3);
    }
    row[0] = 0;
    return row;
  });
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", header),
    chunk("IDAT", deflateSync(Buffer.concat(rows))),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

/** A two-node n8n export whose query key, bearer header and body key hold synthetic values. */
function leadIntakeExport(): Buffer {
  const [query, bearer, body] = SYNTHETIC_VALUES;
  const workflow = {
    name: "Lead intake",
    nodes: [
      {
        parameters: { httpMethod: "POST", path: "lead-intake", options: {} },
        id: "6f1c2d3e-0001-4a5b-8c9d-000000000001",
        name: "Webhook",
        type: "n8n-nodes-base.webhook",
        typeVersion: 2,
        position: [0, 0],
      },
      {
        parameters: {
          method: "POST",
          url: `https://api.example.com/v1/leads?api_key=${query}`,
          sendHeaders: true,
          headerParameters: { parameters: [{ name: "Authorization", value: `Bearer ${bearer}` }] },
          sendBody: true,
          bodyParameters: {
            parameters: [
              { name: "email", value: "={{ $json.body.email }}" },
              { name: "apiKey", value: body },
            ],
          },
          options: {},
        },
        id: "6f1c2d3e-0002-4a5b-8c9d-000000000002",
        name: "Create lead",
        type: "n8n-nodes-base.httpRequest",
        typeVersion: 4.2,
        position: [220, 0],
      },
    ],
    connections: { Webhook: { main: [[{ node: "Create lead", type: "main", index: 0 }]] } },
    settings: { executionOrder: "v1" },
    active: false,
  };
  return Buffer.from(JSON.stringify(workflow, null, 2));
}

/** What a spec attaches on the start page and checks once the Project's first turn ran. */
export interface StartAttachments {
  /** Wraps the spec's first-turn choice: after it, the files are attached and ready, and the creation is observed. */
  choose: (choose: FirstTurnChoice) => FirstTurnChoice;
  /**
   * Checks, on the Project page the creation opened, the Session's first message, the Project's downloads of
   * both files once the Session showed them to the model, and their chips in the chat.
   */
  expectDelivered: (page: Page, projectId: string) => Promise<void>;
}

export function startAttachments(): StartAttachments {
  const image = processDiagram();
  const exported = leadIntakeExport();
  let creation: Promise<Request> | null = null;
  return {
    choose: (choose) => async (page) => {
      await choose(page);
      const puts: Response[] = [];
      const onResponse = (response: Response) => {
        if (response.request().method() === "PUT") puts.push(response);
      };
      page.on("response", onResponse);
      await page.getByTestId("attach-files-input").setInputFiles([
        { name: IMAGE_NAME, mimeType: "image/png", buffer: image },
        { name: EXPORT_NAME, mimeType: "application/json", buffer: exported },
      ]);
      const ready = page.locator('[data-attachment-chip="ready"]');
      await expect(ready).toHaveCount(2, { timeout: 120_000 });
      page.off("response", onResponse);
      // Each file went to storage through its presigned PUT, never through Studio.
      expect(puts.map((response) => response.status())).toEqual([200, 200]);
      for (const response of puts) expect(new URL(response.url()).origin).not.toBe(studioOrigin());
      await expect(page.getByRole("button", { name: new RegExp(`^${EXPORT_NAME}, JSON, .*, ready$`) })).toBeVisible();
      await expect(
        page.locator('[data-attachment-chip="ready"]').getByText(/^n8n workflow · 2 nodes · \d+ keys removed$/),
      ).toBeVisible();
      await expect(page.getByText("Files are read by the AI model to understand your request.")).toBeVisible();
      creation = page.waitForRequest(rpcPosted("CreateProject"));
    },
    expectDelivered: async (page, projectId) => {
      if (!creation) throw new Error("The start page attached no files");
      const request: RpcInput<"CreateProject"> = (await creation).postDataJSON();
      const ids = request.initialAttachmentIds ?? [];
      expect(ids, "CreateProject carries both ready attachments").toHaveLength(2);
      // The first message joins the history once the agent takes it.
      const firstMessage = async () =>
        ((await readSession(page, projectId)).agent?.history?.messages ?? []).find(
          (entry) => entry.message?.role === "ROLE_USER" && (entry.message.attachments ?? []).length > 0,
        )?.message;
      await expect.poll(firstMessage, { timeout: 10 * 60_000, intervals: [1000] }).toBeTruthy();
      const first = await firstMessage();
      expect(first?.content).toBe(request.initialMessage);
      const shown = first?.attachments ?? [];
      expect(shown.map((attachment) => attachment.attachmentId)).toEqual(ids);
      const [png, json] = shown;
      expect([png.fileName, png.kind, png.width, png.height]).toEqual([IMAGE_NAME, "ATTACHMENT_KIND_IMAGE", 96, 32]);
      expect([json.fileName, json.kind, json.jsonFormat]).toEqual([EXPORT_NAME, "ATTACHMENT_KIND_JSON", "n8n"]);
      expect(json.redactions).toBe(SYNTHETIC_VALUES.length);
      // The Session copied both into the Project when it showed them, so both have short-lived downloads.
      const downloads = async (disposition: "ATTACHMENT_DISPOSITION_INLINE" | "ATTACHMENT_DISPOSITION_DOWNLOAD") =>
        (await rpcResult(page.request, "GetAttachmentDownloads", { projectId, attachmentIds: ids, disposition }))
          .downloads;
      await expect
        .poll(async () => (await downloads("ATTACHMENT_DISPOSITION_INLINE")).length, {
          timeout: 5 * 60_000,
          intervals: [1000],
        })
        .toBe(2);
      const [inlineImage] = await downloads("ATTACHMENT_DISPOSITION_INLINE");
      const imageGet = await page.request.get(inlineImage.url);
      expect(imageGet.status()).toBe(200);
      expect(Buffer.from(await imageGet.body()).equals(image), "the inline image is the uploaded PNG").toBe(true);
      const exportGet = await page.request.get((await downloads("ATTACHMENT_DISPOSITION_DOWNLOAD"))[1].url);
      expect(exportGet.status()).toBe(200);
      expect(exportGet.headers()["content-disposition"]).toContain(EXPORT_NAME);
      const redacted = await exportGet.text();
      expect(redacted).toContain(REDACTION_MARKER);
      for (const value of SYNTHETIC_VALUES)
        expect(redacted.includes(value), "a secret-shaped value is removed").toBe(false);
      // The chat the creation opened shows both files on the first message; the image with its thumbnail.
      const imageChip = page.getByRole("button", { name: new RegExp(`^${IMAGE_NAME}, image, .*\\. Download$`) });
      await expect(imageChip).toBeVisible({ timeout: 60_000 });
      await expect(imageChip.locator("img")).toHaveAttribute("src", /^https?:\/\//, { timeout: 120_000 });
      await expect(page.getByRole("button", { name: new RegExp(`^${EXPORT_NAME}, n8n workflow, `) })).toBeVisible();
    },
  };
}
