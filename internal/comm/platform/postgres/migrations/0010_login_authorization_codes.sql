-- Copyright (c) 2026 Super Durable, Inc.
--
-- Licensed under the Sustainable Use License 1.0.
-- You may not use this file except in compliance with the License.
-- See the LICENSE file in the repository root.
--
-- SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

ALTER TABLE login_sessions
  ADD COLUMN audience TEXT NOT NULL DEFAULT 'studio'
    CHECK (audience IN ('login', 'studio', 'engine'));

CREATE TABLE login_authorization_codes (
  code_hash BYTEA PRIMARY KEY CHECK (octet_length(code_hash) = 32),
  account_id TEXT NOT NULL REFERENCES accounts (account_id),
  issuer_session_id TEXT NOT NULL REFERENCES login_sessions (session_id) ON DELETE CASCADE,
  product_origin TEXT NOT NULL CHECK (octet_length(product_origin) <= 512),
  redirect_uri TEXT NOT NULL CHECK (octet_length(redirect_uri) <= 512),
  code_challenge BYTEA NOT NULL CHECK (octet_length(code_challenge) = 32),
  expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX login_authorization_codes_issuer_expiry
  ON login_authorization_codes (issuer_session_id, expires_at);

-- SELECT FOR UPDATE during redemption requires UPDATE privilege as well.
GRANT SELECT, INSERT, UPDATE, DELETE ON login_authorization_codes TO superverse_server;
