// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { NextConfig } from "next";
import path from "node:path";

const nextConfig: NextConfig = {
  output: "standalone",
  // Source maps let the E2E coverage of a spec name the Studio source files it ran; never on in a hosted build.
  productionBrowserSourceMaps: process.env.SUPERVERSE_STUDIO_SOURCE_MAPS === "true",
  outputFileTracingRoot: path.resolve(process.cwd(), "../.."),
  outputFileTracingIncludes: { "/*": ["../../api/superverse/v2/superverse.proto", "./proto/google/rpc/*.proto"] },
  // Next's dev server treats 127.0.0.1 as a different host from localhost and
  // blocks its own dev chunks, which leaves the page rendered but never
  // hydrated. Both spellings of "this machine" are the same machine.
  allowedDevOrigins: ["127.0.0.1", "localhost"],
  // Keep the Next.js Dev Tools badge clear of the chat composer.
  devIndicators: {
    position: "bottom-right",
  },
};

export default nextConfig;
