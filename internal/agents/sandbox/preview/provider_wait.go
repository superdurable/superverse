// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package preview

import (
	"errors"
	"time"

	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/projects/infra"
)

const (
	// operationBudget is the wait limit of each provider wait of one admitted
	// command; the engine enforces it through PollingStepOptions.
	operationBudget = 30 * time.Minute
	// environmentBudget is the wait limit of a Preview engine start, which
	// resumes its state from the volume.
	environmentBudget = 10 * time.Minute
	// environmentStopBudget is the wait limit of a graceful Preview engine
	// stop before the Pod deletion stops it: the supervisor gives each of its
	// three process groups ten seconds before it kills them.
	environmentStopBudget = 3 * time.Minute
	// providerInterval spaces the rounds of a provider wait; a user usually
	// watches it.
	providerInterval = 2 * time.Second
	// providerRoundTimeout bounds one round of a provider wait, which makes
	// several Kubernetes, sandboxd, storage or Project calls.
	providerRoundTimeout = 40 * time.Second
	// previewSourceTimeout bounds one source preparation attempt: the
	// generator's own four minutes plus the inspection, the manifest
	// registration and the bundle storage around it.
	previewSourceTimeout = 6 * time.Minute
	// workspaceWaitBudget bounds the wait for the Session's workspace request
	// to settle. It outlasts the Session's own bounded wait for a
	// provisioning, after which the Session settles the request either way.
	workspaceWaitBudget = 90 * time.Minute
)

// providerWait is what one Step observes while it polls a provider: a
// definite refusal that ends the wait. The engine ends a wait that outlasts
// the Step's wait limit, and RecordPreviewFailure records it.
type providerWait struct {
	// code and refusal record an answer that asking again cannot change, and
	// owner who must act on it; the zero owner is the platform.
	code    failureCode
	refusal error
	owner   conversation.DeliveryOwner
}

// refuse ends the wait with the failure code and its cause, which the platform
// owns.
func (w *providerWait) refuse(code failureCode, cause error) {
	w.code, w.refusal = code, cause
}

// refuseApplication ends the wait with the failure code of the application's
// own failure and its cause.
func (w *providerWait) refuseApplication(code failureCode, cause error) {
	w.refuse(code, cause)
	w.owner = conversation.DeliveryOwnerApplication
}

// refuseGrant is the end of a round whose storage grant failed with err. A
// grant that names too many secrets is the configuration's to fix, an
// ownership conflict is refused with code, and any other error is asked again.
func (w *providerWait) refuseGrant(code failureCode, err error) (bool, error) {
	if errors.Is(err, infra.ErrGrantTooLarge) {
		w.refuse(failurePreviewConfiguration, err)
		w.owner = conversation.DeliveryOwnerConfiguration
		return true, nil
	}
	return w.refuseConflict(code, err)
}

// refuseIf is the end of a round that failed with err. A definite answer,
// which its classification gave a code, refuses the wait with that code; any
// other error ends the attempt, so the Step's retry policy asks again.
func (w *providerWait) refuseIf(code failureCode, err error) (bool, error) {
	if code != noFailure {
		w.refuse(code, err)
		return true, nil
	}
	return false, err
}

// refuseConflict is the end of a round that failed with err, of which only an
// ownership conflict is a definite answer, refused with code.
func (w *providerWait) refuseConflict(code failureCode, err error) (bool, error) {
	if !errors.Is(err, infra.ErrOwnershipConflict) {
		code = noFailure
	}
	return w.refuseIf(code, err)
}

// failure is the refusal a finished wait records: its definite refusal.
// failed is false when the provider became ready.
func (w *providerWait) failure() (refused previewRefusal, failed bool) {
	if w.refusal == nil {
		return previewRefusal{}, false
	}
	refused = refusal(w.code, w.refusal)
	if w.owner != (conversation.DeliveryOwner{}) {
		refused.Owner = w.owner
	}
	return refused, true
}
