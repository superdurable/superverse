// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import NextLink from "next/link";
import type { ComponentProps } from "react";
import { useFlowNavigation } from "@/lib/flow/FlowNavigationProvider";
import type { FlowTarget } from "@/lib/shared/studio-routes";

export function FlowLink({ to, ...rest }: { to: FlowTarget } & Omit<ComponentProps<typeof NextLink>, "href">) {
  const { href } = useFlowNavigation();
  return <NextLink href={href(to)} {...rest} />;
}
