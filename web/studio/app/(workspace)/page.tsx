// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { PageBar } from "@/components/shell/PageBar";
import { TeamSpaceLanding } from "@/components/teamspace/TeamSpaceLanding";

export default function HomePage() {
  return (
    <>
      <PageBar title="Home" />
      <TeamSpaceLanding />
    </>
  );
}
