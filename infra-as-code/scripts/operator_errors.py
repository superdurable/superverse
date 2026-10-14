# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Operator-facing failures.

sv2 prints the message of a RuntimeError and replaces every other exception with
one fixed sentence, because parser and provider errors may quote private data.
"""


def require(condition, message):
    if not condition:
        raise RuntimeError(message)
