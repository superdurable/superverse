// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
)

const (
	tokenFile    = "token"
	recordFile   = "binding.json"
	progressFile = "progress.json"
	reportsDir   = "reports"
)

// The unacknowledged report store of one binding holds at most this many
// reports and bytes; when full the Bridge accepts no new MCP calls.
const (
	MaxStoredReports     = 1024
	MaxStoredReportBytes = 8 << 20
)

// ErrStoreFull is returned by StoreReports while the store is at a bound.
var ErrStoreFull = errors.New("the Bridge holds too many unacknowledged reports; wait for the platform to acknowledge them")

// BindingRecord names the connection and the provider's stable local session.
// Empty ProviderID is an existing Orca record; its JSON keys stay unchanged.
type BindingRecord struct {
	Endpoint         string    `json:"endpoint"`
	ConnectionID     string    `json:"connectionId"`
	ProjectID        string    `json:"projectId"`
	ProjectName      string    `json:"projectName"`
	ProviderID       string    `json:"providerId,omitempty"`
	SessionID        string    `json:"orcaSessionId"`
	ProviderThreadID string    `json:"providerThreadId"`
	ProviderVersion  string    `json:"orcaVersion"`
	AgentVersion     string    `json:"codexVersion"`
	HostLabel        string    `json:"hostLabel"`
	PairedAt         time.Time `json:"pairedAt"`
}

// Progress is how far one binding got. The relay owns its meaning; this
// package only keeps it durable.
type Progress struct {
	// Activated is false until the first activation answered.
	Activated  bool   `json:"activated"`
	Epoch      uint64 `json:"epoch"`
	Generation uint64 `json:"generation"`
	// Boundary is the provider event position at which Generation was adopted;
	// zero until the first adoption.
	Boundary EventPosition `json:"boundary"`
	// EventCursor is where the event loop resumes reading provider events.
	EventCursor EventPosition `json:"eventCursor"`
	// Cursor is the last conversation entry the delivery loop handled.
	Cursor uint64 `json:"cursor"`
	// BriefActivationCursor caps recent messages in the brief, even after delivery advances Cursor.
	BriefActivationCursor uint64   `json:"briefActivationCursor,omitempty"`
	Sending               *Sending `json:"sending,omitempty"`
	// Acknowledged is the highest report sequence the platform admitted; the
	// next report is Acknowledged+1.
	Acknowledged          uint64 `json:"acknowledged"`
	LastImportedMessageID string `json:"lastImportedMessageId,omitempty"`
	HistoryImported       bool   `json:"historyImported"`
	BriefSent             bool   `json:"briefSent"`
	// BriefAccepted distinguishes a proved first brief from an attempted
	// brief whose one-write submission was uncertain. The relay owns it.
	BriefAccepted            bool      `json:"briefAccepted,omitempty"`
	BriefWaitStartedAt       time.Time `json:"briefWaitStartedAt,omitzero"`
	BriefText                string    `json:"briefText,omitempty"`
	BriefWithoutContext      bool      `json:"briefWithoutContext,omitempty"`
	ContextFollowupSent      bool      `json:"contextFollowupSent,omitempty"`
	ContextFollowupAbandoned bool      `json:"contextFollowupAbandoned,omitempty"`
	ContextFollowupStartedAt time.Time `json:"contextFollowupStartedAt,omitzero"`
	ContextFollowupCheckedAt time.Time `json:"contextFollowupCheckedAt,omitzero"`
	ContextFollowupText      string    `json:"contextFollowupText,omitempty"`
	// Turns binds recent provider turns to the generation they belong to.
	Turns []TurnGeneration `json:"turns,omitempty"`
	// Delivered holds the provider message IDs of recent deliveries with the
	// generation each was sent for: the event loop does not report them back,
	// and a turn they open belongs to that generation.
	Delivered []DeliveredMessage `json:"delivered,omitempty"`
	// Draining is the work a restart found, until it settled.
	Draining *Draining `json:"draining,omitempty"`
}

// Draining is the work a restart found: the reports stored through Slot and,
// when TurnID is set, the latest Codex turn already read, stopped unless it
// was over. An empty TurnID names no turn (the latest one was never read).
// CompletionStored records that the named turn's completion report was
// stored, in the same write. Admitted is the highest acknowledged report
// sequence the drain waits to see settled: the acknowledged sequence when the
// record was made, raised by each acknowledgement of a drained report in the
// write that records it. A record without slot reads 0 and drains its turn.
type Draining struct {
	TurnID           string `json:"turnId"`
	CompletionStored bool   `json:"completionStored"`
	Admitted         uint64 `json:"admitted"`
	Slot             uint64 `json:"slot,omitempty"`
}

// EventPosition stores a provider-owned resume point. Epoch and Sequence retain
// the existing Orca JSON format; direct providers use Cursor instead.
type EventPosition struct {
	Epoch    string `json:"epoch"`
	Sequence uint64 `json:"sequence"`
	Cursor   string `json:"cursor,omitempty"`
}

// IsZero reports a position that was never recorded.
func (position EventPosition) IsZero() bool { return position == EventPosition{} }

// Sending is the delivery in flight. Journal providers can query the same
// operation again; one-write providers persist Attempted before the only write.
type Sending struct {
	Key           string `json:"key"`
	EntrySequence uint64 `json:"entrySequence"`
	Generation    uint64 `json:"generation"`
	OperationID   string `json:"operationId"`
	Fingerprint   string `json:"fingerprint"`
	Marker        string `json:"marker,omitempty"`
	OneWrite      bool   `json:"oneWrite,omitempty"`
	Attempted     bool   `json:"attempted,omitempty"`
	// InstanceID identifies the Orca journal process of the first submission.
	InstanceID  string    `json:"runtimeId"`
	FirstSentAt time.Time `json:"firstSentAt"`
}

// TurnGeneration binds one Codex turn to a work generation. Completed
// records that the event cursor passed the turn's completion, whose report
// was stored (or dropped as stale) in that write.
type TurnGeneration struct {
	TurnID     string `json:"turnId"`
	Generation uint64 `json:"generation"`
	Completed  bool   `json:"completed,omitempty"`
}

// DeliveredMessage is an Orca message the Bridge delivered.
type DeliveredMessage struct {
	MessageID  string `json:"messageId"`
	Generation uint64 `json:"generation"`
}

// Binding is one (Project, Orca session) binding with its files.
type Binding struct {
	Record BindingRecord
	path   string

	mu          sync.Mutex
	storedCount int
	storedBytes int64
	// lastSlot is the last committed report slot: progress.json records it,
	// so a slot file after it is a write that never committed.
	lastSlot uint64
}

// progressRecord is progress.json: the relay's Progress and the last report
// slot it committed together with it.
type progressRecord struct {
	Progress
	// ReportSlot is absent only in a file written before it existed, when
	// every stored slot counts as committed.
	ReportSlot *uint64 `json:"reportSlot,omitempty"`
}

func loadBinding(path string) (*Binding, error) {
	binding := &Binding{path: path}
	if err := readJSON(filepath.Join(path, recordFile), &binding.Record); err != nil {
		return nil, err
	}
	record, err := binding.progressRecord()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(path, reportsDir))
	if err != nil {
		return nil, fmt.Errorf("list stored reports: %w", err)
	}
	committed := uint64(0)
	for _, entry := range entries {
		if slot, ok := reportSlot(entry.Name()); ok {
			committed = max(committed, slot)
		}
	}
	if record.ReportSlot != nil {
		committed = *record.ReportSlot
	}
	binding.lastSlot = committed
	for _, entry := range entries {
		slot, ok := reportSlot(entry.Name())
		if !ok || slot > committed {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("stat stored report: %w", err)
		}
		binding.storedCount++
		binding.storedBytes += info.Size()
	}
	return binding, nil
}

// Token reads the connection token.
func (binding *Binding) Token() (string, error) {
	data, err := os.ReadFile(filepath.Join(binding.path, tokenFile))
	if err != nil {
		return "", fmt.Errorf("read the connection token: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// Progress reads the binding's progress; a new binding has none yet.
func (binding *Binding) Progress() (Progress, error) {
	record, err := binding.progressRecord()
	return record.Progress, err
}

func (binding *Binding) progressRecord() (progressRecord, error) {
	var record progressRecord
	err := readJSON(filepath.Join(binding.path, progressFile), &record)
	if errors.Is(err, os.ErrNotExist) {
		return progressRecord{}, nil
	}
	return record, err
}

// SaveProgress replaces the progress in one atomic write.
func (binding *Binding) SaveProgress(progress Progress) error {
	binding.mu.Lock()
	defer binding.mu.Unlock()
	return binding.saveProgressLocked(progress, binding.lastSlot)
}

func (binding *Binding) saveProgressLocked(progress Progress, lastSlot uint64) error {
	return writeJSON(filepath.Join(binding.path, progressFile), progressRecord{Progress: progress, ReportSlot: &lastSlot})
}

// StoredReport is an unacknowledged report in its slot. Slots order the
// reports; only the report being sent has its sequence.
type StoredReport struct {
	Slot   uint64
	Report bridgeapi.Report
}

// StoreReports keeps reports until the platform acknowledges or refuses
// them, in the slots after every stored one, and saves progress with them:
// the reports and the progress that accounts for them commit in the one
// atomic write of progress.json, which records the last committed slot. A
// crash before it leaves slot files that are never read and are overwritten
// later. ErrStoreFull, returned at a bound unless the store is empty (so the
// reports of one commit always fit eventually), leaves everything unchanged.
func (binding *Binding) StoreReports(progress Progress, reports ...bridgeapi.Report) ([]uint64, error) {
	data := make([][]byte, len(reports))
	added := int64(0)
	for index, report := range reports {
		data[index] = comm.Must(json.Marshal(report))
		added += int64(len(data[index]))
	}
	binding.mu.Lock()
	defer binding.mu.Unlock()
	if binding.storedCount > 0 && (binding.storedCount+len(reports) > MaxStoredReports || binding.storedBytes+added > MaxStoredReportBytes) {
		return nil, ErrStoreFull
	}
	slots := make([]uint64, len(reports))
	for index := range reports {
		slots[index] = binding.lastSlot + uint64(index) + 1
		if err := writeFile(binding.reportPath(slots[index]), data[index]); err != nil {
			return nil, err
		}
	}
	lastSlot := binding.lastSlot + uint64(len(reports))
	if err := binding.saveProgressLocked(progress, lastSlot); err != nil {
		return nil, err
	}
	binding.storedCount += len(reports)
	binding.storedBytes += added
	binding.lastSlot = lastSlot
	return slots, nil
}

// UpdateReport rewrites a stored report, such as when it gets its sequence.
func (binding *Binding) UpdateReport(slot uint64, report bridgeapi.Report) error {
	data := comm.Must(json.Marshal(report))
	binding.mu.Lock()
	defer binding.mu.Unlock()
	path := binding.reportPath(slot)
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat report slot %d: %w", slot, err)
	}
	if err := writeFile(path, data); err != nil {
		return err
	}
	binding.storedBytes += int64(len(data)) - info.Size()
	return nil
}

// StoredReports returns the unacknowledged reports in slot order.
func (binding *Binding) StoredReports() ([]StoredReport, error) {
	binding.mu.Lock()
	defer binding.mu.Unlock()
	slots, err := binding.storedSlots()
	if err != nil {
		return nil, err
	}
	reports := make([]StoredReport, 0, len(slots))
	for _, slot := range slots {
		stored := StoredReport{Slot: slot}
		if err := readJSON(binding.reportPath(slot), &stored.Report); err != nil {
			return nil, err
		}
		reports = append(reports, stored)
	}
	return reports, nil
}

// RemoveReport drops an acknowledged or refused report.
func (binding *Binding) RemoveReport(slot uint64) error {
	binding.mu.Lock()
	defer binding.mu.Unlock()
	path := binding.reportPath(slot)
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat report slot %d: %w", slot, err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove report slot %d: %w", slot, err)
	}
	binding.storedCount--
	binding.storedBytes -= info.Size()
	return nil
}

// Remove deletes the binding with its token and stored reports.
func (binding *Binding) Remove() error {
	if err := os.RemoveAll(binding.path); err != nil {
		return fmt.Errorf("remove the binding: %w", err)
	}
	return nil
}

func (binding *Binding) storedSlots() ([]uint64, error) {
	entries, err := os.ReadDir(filepath.Join(binding.path, reportsDir))
	if err != nil {
		return nil, fmt.Errorf("list stored reports: %w", err)
	}
	var slots []uint64
	for _, entry := range entries {
		if slot, ok := reportSlot(entry.Name()); ok && slot <= binding.lastSlot {
			slots = append(slots, slot)
		}
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i] < slots[j] })
	return slots, nil
}

func (binding *Binding) reportPath(slot uint64) string {
	return filepath.Join(binding.path, reportsDir, strconv.FormatUint(slot, 10)+".json")
}

func reportSlot(name string) (uint64, bool) {
	text, found := strings.CutSuffix(name, ".json")
	if !found {
		return 0, false
	}
	slot, err := strconv.ParseUint(text, 10, 64)
	return slot, err == nil
}

func writeJSON(path string, value any) error {
	return writeFile(path, comm.Must(json.MarshalIndent(value, "", "  ")))
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path) //nolint:gosec // a file below the Bridge's private state directory
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	if err := comm.DecodeStrictJSON(data, target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
