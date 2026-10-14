// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { arrayField, isNonEmptyString, isRecord } from "@/lib/shared/json";

interface SpreadsheetFile {
  id: string;
  name: string;
}

export function parseSpreadsheetPage(value: Record<string, unknown>): {
  files: SpreadsheetFile[];
  nextPageToken: string;
} {
  const files = arrayField(value.files).flatMap((item) =>
    isRecord(item) && isNonEmptyString(item.id) && isNonEmptyString(item.name)
      ? [{ id: item.id, name: item.name }]
      : [],
  );
  return { files, nextPageToken: isNonEmptyString(value.nextPageToken) ? value.nextPageToken : "" };
}

export function parseSheetTabs(value: Record<string, unknown>): string[] {
  return arrayField(value.sheets).flatMap((sheet) => {
    if (!isRecord(sheet) || !isRecord(sheet.properties) || !isNonEmptyString(sheet.properties.title)) return [];
    return [sheet.properties.title];
  });
}
