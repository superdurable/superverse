// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package boundary

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// protoEnum is a generated protobuf enum.
type protoEnum interface {
	~int32
	protoreflect.Enum
}

// Enum is the E value of a persisted domain value: the value whose name is
// E's prefix and the upper-cased domain value. An empty domain value is the
// zero value. Every write of a domain value is in E's inventory, so any other
// value is a programming error.
func Enum[E protoEnum, D ~string](domain D) E {
	if domain == "" {
		return 0
	}
	var zero E
	values := zero.Descriptor().Values()
	name := enumPrefix(values) + strings.ToUpper(string(domain))
	value := values.ByName(protoreflect.Name(name))
	if value == nil {
		panic(fmt.Sprintf("%s declares no value %s", zero.Descriptor().FullName(), name))
	}
	return E(value.Number())
}

// enumPrefix is the prefix of every value name of an enum, which Buf lint
// enforces: its zero value's name without UNSPECIFIED.
func enumPrefix(values protoreflect.EnumValueDescriptors) string {
	return strings.TrimSuffix(string(values.ByNumber(0).Name()), "UNSPECIFIED")
}

// FailureSnapshot is the public view of a Flow's failure record with the
// remedy its code declared, for a failure no owner is recorded with.
func FailureSnapshot(f *dexflow.Failure) *pb.Failure {
	return OwnedFailureSnapshot(f, conversation.DeliveryOwner{})
}

// OwnedFailureSnapshot is FailureSnapshot with owner, who must act on it.
func OwnedFailureSnapshot(f *dexflow.Failure, owner conversation.DeliveryOwner) *pb.Failure {
	if f == nil {
		return nil
	}
	return &pb.Failure{Code: f.Code, Phase: f.Phase, Message: f.Message, IsRecoverable: f.Recoverable,
		Remedy: Enum[pb.FailureRemedy](f.Remedy.String()), Owner: Enum[pb.FailureOwner](owner.String())}
}

// PhaseTimingSnapshots is the public view of the phases a Flow measured, in
// the order it recorded them. A zero duration marks a phase in progress, so an
// ended phase rounds up to whole milliseconds and never reads as running.
func PhaseTimingSnapshots(timings []dexflow.PhaseTiming) []*pb.PhaseTiming {
	out := make([]*pb.PhaseTiming, 0, len(timings))
	for _, t := range timings {
		durationMs := int64((t.Duration + time.Millisecond - 1) / time.Millisecond)
		out = append(out, &pb.PhaseTiming{Phase: t.Phase, StartedAt: timestamppb.New(t.StartedAt), DurationMs: durationMs})
	}
	return out
}

// CommitVersionSnapshot is the public view of a commit's version, or nil when
// none was recorded.
func CommitVersionSnapshot(version *repohost.CommitVersion) *pb.CommitVersion {
	if version == nil {
		return nil
	}
	return &pb.CommitVersion{Number: version.Number, Subject: version.Subject}
}

// OptionalTimestamp leaves an unset time unset.
func OptionalTimestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}

// FlowDefinitionSnapshots converts Flow Definitions that their owner read
// through appmanifest validation, which accepts only an FDG 2.0 JSON object
// as a graph; converting such a graph cannot fail.
func FlowDefinitionSnapshots(definitions []appmanifest.FlowDefinition) []*pb.FlowDefinition {
	out := make([]*pb.FlowDefinition, 0, len(definitions))
	for _, d := range definitions {
		var graph map[string]any
		comm.MustDo(json.Unmarshal(d.Graph, &graph))
		out = append(out, &pb.FlowDefinition{FlowType: d.FlowType, Definition: comm.Must(structpb.NewStruct(graph)), SourcePath: d.SourcePath, SchemaVersion: d.SchemaVersion, Digest: d.Digest, Valid: d.Valid})
	}
	return out
}
