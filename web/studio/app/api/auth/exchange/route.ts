// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { exchangeLogin } from "@/lib/server/login-host/handoff-handler";

export const POST = exchangeLogin;
export const dynamic = "force-dynamic";
export const runtime = "nodejs";
