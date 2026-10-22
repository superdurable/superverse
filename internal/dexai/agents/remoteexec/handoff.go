// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package remoteexec

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// HandoffNoticeLabel is the visible label of the notice that a return to the platform agent records.
const HandoffNoticeLabel = "Switched to the Platform agent"

// HandoffNoticePrefix identifies public context, never executor authority.
const HandoffNoticePrefix = "Switched to the Superverse agent. Remote executor owners: "

// MaximumHandoffBytes bounds the whole notice, including its optional sections.
const MaximumHandoffBytes = 16 << 10

const maximumHandoffOwners = 4

const handoffOlderWorkNotice = "Only the recent part of that work is in the context above; older work is in the repository and its commits."

// HandoffNotice is the parsed first line of a transient context notice.
type HandoffNotice struct {
	owners []string
	from   time.Time
	to     time.Time
	reason HandoffReason
	Head   string
}

// Notice renders the canonical metadata and older-work notice before bounded sections.
func (handoff PlatformHandoff) Notice(head string) string {
	owners := make([]string, 0, len(handoff.Owners))
	for _, owner := range handoff.Owners {
		owners = append(owners, owner.Email)
	}
	text := fmt.Sprintf(HandoffNoticePrefix+"%s. Remote interval: %s to %s. Reason: %s. HEAD: %s.",
		strings.Join(owners, ", "), handoff.From.Format(time.RFC3339), handoff.To.Format(time.RFC3339), handoff.Reason, head) + "\n" + handoffOlderWorkNotice
	if _, err := ParseHandoffNotice(text); err != nil {
		panic(err) // Persisted authenticated metadata violating its format is a programming error.
	}
	return text
}

// ParseHandoffNotice validates the format shared by the renderer and replay assertions.
// Sections remain intact; the metadata and exact older-work second line are required.
func ParseHandoffNotice(text string) (HandoffNotice, error) {
	var notice HandoffNotice
	if len(text) > MaximumHandoffBytes || !utf8.ValidString(text) {
		return notice, errors.New("executor handoff exceeds its UTF-8 byte bound")
	}
	line, rest, hasNotice := strings.Cut(text, "\n")
	olderWork, _, _ := strings.Cut(rest, "\n")
	if !hasNotice || olderWork != handoffOlderWorkNotice {
		return notice, errors.New("executor handoff lacks its canonical older-work notice")
	}
	line, found := strings.CutPrefix(line, HandoffNoticePrefix)
	if !found {
		return notice, errors.New("executor handoff lacks its canonical prefix")
	}
	owners, rest, found := strings.Cut(line, ". Remote interval: ")
	notice.owners = strings.Split(owners, ", ")
	if !found || len(notice.owners) > maximumHandoffOwners {
		return notice, errors.New("executor handoff lacks bounded owners")
	}
	for _, owner := range notice.owners {
		if owner == "" || len(owner) > 254 || strings.TrimSpace(owner) != owner || strings.ContainsAny(owner, "\r\n") {
			return notice, errors.New("executor handoff owner is invalid")
		}
	}
	interval, rest, found := strings.Cut(rest, ". Reason: ")
	from, to, split := strings.Cut(interval, " to ")
	var firstErr, lastErr error
	notice.from, firstErr = time.Parse(time.RFC3339, from)
	notice.to, lastErr = time.Parse(time.RFC3339, to)
	if !found || !split || firstErr != nil || lastErr != nil || notice.from.IsZero() || notice.to.Before(notice.from) {
		return notice, errors.New("executor handoff interval is invalid")
	}
	reason, head, found := strings.Cut(rest, ". HEAD: ")
	notice.reason = HandoffReason(reason)
	if !found || notice.reason != HandoffSwitched && notice.reason != HandoffConnectionRemoved || !strings.HasSuffix(head, ".") {
		return notice, errors.New("executor handoff reason or HEAD is invalid")
	}
	notice.Head = strings.TrimSuffix(head, ".")
	decoded, err := hex.DecodeString(notice.Head)
	if err != nil || len(decoded) != 20 || len(notice.Head) != 40 || hex.EncodeToString(decoded) != notice.Head {
		return notice, errors.New("executor handoff HEAD is invalid")
	}
	return notice, nil
}
