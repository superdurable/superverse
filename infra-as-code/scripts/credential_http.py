# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""HTTP for requests that carry a credential.

urllib's default redirect handler copies the Authorization header to the redirect target, so these
requests refuse redirects; a caller that expects one follows it explicitly, without the credential.
"""
import urllib.parse
import urllib.request

from operator_errors import require


class _RefuseRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def credential_opener():
    """An opener that surfaces every redirect as an HTTPError instead of following it."""
    return urllib.request.build_opener(_RefuseRedirect)


def redirect_target(location, host_allowed, message):
    """The redirect Location to follow without the credential: HTTPS on the default port to an allowed host, with no userinfo."""
    parsed = urllib.parse.urlsplit(location)
    require(parsed.scheme == "https" and parsed.hostname and host_allowed(parsed.hostname) and parsed.port in (None, 443) and
            not parsed.username and not parsed.password, message)
    return location
