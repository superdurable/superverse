// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package boundary

import (
	"fmt"

	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PageRequest is the paging that every List RPC accepts.
type PageRequest interface {
	GetPageSize() int32
	GetPageToken() string
}

// Page is a validated page request of a List RPC whose tokens are bound to
// one scope: the caller, the resource and the filters.
type Page struct {
	scope string
	Size  int
	After *postgres.PageKey
}

// ReadPage validates the paging of a List RPC: 50 rows when the size is
// unset, the first page when the token is.
func (b *Boundary) ReadPage(scope string, in PageRequest) (Page, error) {
	size := in.GetPageSize()
	if size == 0 {
		size = 50
	}
	if size < 1 || size > 100 {
		return Page{}, status.Error(codes.InvalidArgument, "page size must be between 1 and 100")
	}
	page := Page{scope: fmt.Sprintf("%s/%d", scope, size), Size: int(size)}
	token, err := b.Cursors.DecodePage(page.scope, in.GetPageToken())
	if err != nil {
		return Page{}, status.Error(codes.InvalidArgument, "invalid page token")
	}
	if token != "" {
		after, err := postgres.ParsePageKey(token)
		if err != nil {
			return Page{}, status.Error(codes.InvalidArgument, "invalid page token")
		}
		page.After = &after
	}
	return page, nil
}

// NextPageToken signs the token of the page after next, or returns none when
// the page is the last.
func (b *Boundary) NextPageToken(page Page, next *postgres.PageKey) string {
	if next == nil {
		return ""
	}
	return b.Cursors.EncodePage(page.scope, next.String())
}
