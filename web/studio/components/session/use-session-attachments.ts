// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useCallback, useMemo } from "react";
import { useProjectResource } from "@/lib/project/ProjectResourceProvider";
import type { MessageAttachment } from "@/lib/session/agent-types";
import { projectAttachmentLinks } from "@/lib/session/attachments/attachment-downloads";
import { LOCAL_REFUSALS } from "@/lib/session/attachments/attachment-files";
import { useAttachmentTray } from "@/lib/session/attachments/use-attachment-tray";

/**
 * The Session chat's attachments: the composer's tray, uploading into the Project's Teamspace and remembered
 * per Project, the links through which the conversation shows sent files, and how the chips of a message
 * taken back from the queue return to the tray.
 */
export function useSessionAttachments(projectId: string, remotelyExecuted: boolean) {
  const teamSpaceId = useProjectResource()?.snapshot?.teamSpaceId || null;
  const tray = useAttachmentTray({
    teamSpaceId,
    scope: projectId,
    disabledReason: remotelyExecuted ? LOCAL_REFUSALS.needsPlatformAgent : null,
  });
  const links = useMemo(() => projectAttachmentLinks(projectId), [projectId]);
  const restore = tray.restore;
  const takeBack = useCallback(
    (attachments: readonly MessageAttachment[]) =>
      restore(
        attachments.map((attachment) => ({
          attachmentId: attachment.attachmentId,
          uploadRequestId: attachment.attachmentId,
          name: attachment.fileName,
          mediaType: attachment.mediaType,
          size: attachment.byteSize,
          sha256: "",
        })),
      ),
    [restore],
  );
  return { tray, links, takeBack };
}
