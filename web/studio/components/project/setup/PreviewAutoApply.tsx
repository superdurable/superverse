// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { usePreviewCommands } from "@/components/build/use-preview-commands";
import { ProblemPanel } from "@/components/ui/problem/ProblemPanel";
import { autoApplyDue, useAutoApply } from "./auto-apply";

const plural = (count: number) => `${count} ${count === 1 ? "change" : "changes"}`;

/**
 * Setup's automatic apply outside Setup: mounted where a person saves Preview's settings, such as Runs' Try it,
 * it updates the Preview once their edits settle, and says so in one line. A refused update shows its problem
 * with Apply again; Preview's own failures stay where Setup and the Interface show them.
 */
export function PreviewAutoApply({ projectId }: { projectId: string }) {
  const commands = usePreviewCommands(projectId);
  const headCommit = commands.session.resource?.headCommit ?? "";
  useAutoApply(commands, headCommit);
  const changes = commands.configuration?.unappliedChangeCount ?? 0;
  if (commands.busy === "apply" || autoApplyDue(commands, headCommit))
    return (
      <p className="text-caption text-accent" role="status" data-testid="preview-auto-apply">
        Updating Preview with {changes > 0 ? plural(changes) : "your settings"}…
      </p>
    );
  if (commands.pushUpdating)
    return (
      <p className="text-caption text-ink-faint" role="status" data-testid="preview-auto-apply">
        Preview is updating to the latest version…
      </p>
    );
  // Only a refused update is this line's to show; a Preview failure is shown where the Preview is.
  if (commands.problem && !commands.preview?.failure)
    return <ProblemPanel problem={commands.problem} actions={commands.remedyActions} />;
  return null;
}
