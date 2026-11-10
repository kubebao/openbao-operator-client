package evidence_test

import (
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/evidence"
)

func TestCollectionAbsenceRequiresCompleteList(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	base := evidence.Collection{
		Scope: evidence.Scope{
			Origin:    "origin",
			Group:     "openbao.org",
			Version:   "v1alpha1",
			Resource:  "openbaorestores",
			Namespace: "team",
			Action:    evidence.ReadList,
		},
		Interval: evidence.Interval{StartedAt: now, CompletedAt: now},
		Outcome:  evidence.CollectionComplete,
	}
	if !base.AbsenceConfirmed() {
		t.Fatal("complete empty list did not establish scoped absence")
	}
	for _, outcome := range []evidence.CollectionOutcome{
		evidence.CollectionPartial, evidence.CollectionDenied, evidence.CollectionUnsupported, evidence.CollectionTimeout,
		evidence.CollectionCancelled, evidence.CollectionPageLimit, evidence.CollectionSizeLimit, evidence.CollectionPaginationLoop,
		evidence.CollectionDeleted, evidence.CollectionReplaced, evidence.CollectionInvalid, evidence.CollectionFailed, "provider CANARY",
	} {
		changed := base
		changed.Outcome = outcome
		if changed.AbsenceConfirmed() {
			t.Fatalf("outcome %s established absence", outcome)
		}
	}
	for _, action := range []evidence.ReadAction{evidence.ReadGet, evidence.ReadDiscovery} {
		changed := base
		changed.Scope.Action = action
		if action == evidence.ReadGet {
			changed.Scope.Name = "restore-1"
		}
		if changed.AbsenceConfirmed() {
			t.Fatal("non-list read established scoped absence")
		}
	}
	for _, count := range []int{-1, 1} {
		changed := base
		changed.Items = count
		if changed.AbsenceConfirmed() {
			t.Fatal("nonempty or invalid count established absence")
		}
	}
	changed := base
	changed.Interval.CompletedAt = now.Add(-time.Second)
	if changed.AbsenceConfirmed() {
		t.Fatal("inverted interval established absence")
	}
}

func TestUnrequestedScopeHasNoAttempt(t *testing.T) {
	t.Parallel()
	value := evidence.Collection{
		Scope:   evidence.Scope{Origin: "origin", Version: "v1", Resource: "pods", Namespace: "team", Action: evidence.ReadList},
		Outcome: evidence.CollectionNotRequested,
	}
	if value.Validate() != nil || value.AbsenceConfirmed() {
		t.Fatal("unrequested scope changed meaning")
	}
	value.Items = 1
	if value.Validate() == nil {
		t.Fatal("unrequested scope retained observations")
	}
}
