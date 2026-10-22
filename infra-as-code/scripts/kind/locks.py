# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""The Kind lane's own locks and its acceptance fence.

Kind serves its loopback ports and Native others, so the lanes run at the same time and neither takes a lock of the
other. Within Kind, one action lock serializes the mutations and the browser acceptance; a failed or interrupted
acceptance keeps its fence, which blocks later mutations until it is reconciled.
"""
from contextlib import contextmanager

from kind.queue import admission
from operator_errors import require
from paths import STATE
from private_files import private_lock

KIND_ACCEPTANCE_DIRECTORY = STATE / "kind-e2e"


def require_no_kind_acceptance():
    fence = KIND_ACCEPTANCE_DIRECTORY / "e2e-admission.json"
    require(not fence.exists(), "Kind acceptance requires explicit process/business reconciliation: " + str(fence))


@contextmanager
def kind_action_lock(action, ticket=None):
    """Serialize Kind mutations and E2E; the long-lived ports and builder take no lock and can serve acceptance."""
    if action in ("ports", "builder"):
        yield
        return
    with private_lock(STATE / "kind-action.lock", message="A Kind mutation or acceptance already owns its action lock"):
        with admission(ticket):
            require_no_kind_acceptance()
            yield
