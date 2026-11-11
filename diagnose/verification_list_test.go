package diagnose_test

import (
	"errors"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/diagnose"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func TestCompleteNamespaceListsVerifyTargetWithoutNamedGet(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*evidence.Cluster, *evidence.Collection, *[]evidence.Cluster)
		state  evidence.VerificationState
	}{
		{"stable", func(_ *evidence.Cluster, _ *evidence.Collection, _ *[]evidence.Cluster) {}, evidence.VerificationVerified},
		{"changed", func(_ *evidence.Cluster, _ *evidence.Collection, values *[]evidence.Cluster) {
			(*values)[0].Generation = integer(5)
		}, evidence.VerificationChanged},
		{"replaced", func(_ *evidence.Cluster, _ *evidence.Collection, values *[]evidence.Cluster) {
			(*values)[0].Reference.Identity.UID = "replacement"
		}, evidence.VerificationReplaced},
		{"deleted", func(_ *evidence.Cluster, read *evidence.Collection, values *[]evidence.Cluster) {
			*values, read.Items = []evidence.Cluster{}, 0
		}, evidence.VerificationDeleted},
		{"deleted-among-other-clusters", func(_ *evidence.Cluster, _ *evidence.Collection, values *[]evidence.Cluster) {
			(*values)[0].Reference.Identity.Name, (*values)[0].Reference.Identity.UID = "other", "other-uid"
		}, evidence.VerificationDeleted},
		{"partial-with-match", func(_ *evidence.Cluster, read *evidence.Collection, _ *[]evidence.Cluster) {
			read.Outcome = evidence.CollectionPartial
		}, evidence.VerificationFailed},
		{"denied", func(_ *evidence.Cluster, read *evidence.Collection, values *[]evidence.Cluster) {
			*values, read.Items, read.Outcome = []evidence.Cluster{}, 0, evidence.CollectionDenied
		}, evidence.VerificationFailed},
		{"unknown-generation", func(_ *evidence.Cluster, _ *evidence.Collection, values *[]evidence.Cluster) {
			(*values)[0].Generation = evidence.Integer{State: evidence.ValueUnknown}
		}, evidence.VerificationInsufficient},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := input(t)
			after := *i.Observation
			at := after.Reference.CollectedAt.Value.Add(time.Second)
			after.Reference.CollectedAt = timestamp(at)
			read := i.Collection
			read.Scope.Action, read.Scope.Name = evidence.ReadList, ""
			read.Interval = evidence.Interval{StartedAt: at, CompletedAt: at}
			values := []evidence.Cluster{after}
			tc.change(i.Observation, &read, &values)
			value, err := evidence.NewTargetVerification(*i.Observation, read, values)
			if err != nil || value.Validate() != nil || value.State != tc.state || value.Read.Scope.Action != evidence.ReadList {
				t.Fatalf("list verification state %s error %v, want %s", value.State, err, tc.state)
			}
			i.Verification = &value
			p := policy(t)
			p.ReferenceTime = at
			report, err := diagnose.Analyze(capture(i), p)
			if err != nil || (tc.state == evidence.VerificationVerified) != (len(report.Findings) > 0) {
				t.Fatal("nonstable list verification produced current findings")
			}
		})
	}
}

func TestListVerificationRejectsFilteredOrIncompleteDeclarations(t *testing.T) {
	t.Parallel()
	i := input(t)
	read := i.Collection
	read.Scope.Action, read.Scope.Name = evidence.ReadList, ""
	for _, change := range []func(*evidence.Collection, *[]evidence.Cluster){
		func(r *evidence.Collection, _ *[]evidence.Cluster) { r.Scope.Selector = "app=bao" },
		func(r *evidence.Collection, _ *[]evidence.Cluster) { r.Scope.Namespace = "" },
		func(r *evidence.Collection, _ *[]evidence.Cluster) { r.Items = 2 },
		func(r *evidence.Collection, values *[]evidence.Cluster) {
			*values = append(*values, (*values)[0])
			r.Items = 2
		},
	} {
		modified, values := read, []evidence.Cluster{*i.Observation}
		change(&modified, &values)
		if _, err := evidence.NewTargetVerification(*i.Observation, modified, values); !errors.Is(err, evidence.ErrInvalidEvidence) {
			t.Fatal("filtered, incomplete, or duplicate list was accepted")
		}
	}
	p := policy(t)
	p.RuleSetVersion = "5"
	if _, err := diagnose.Analyze(capture(i), p); !errors.Is(err, diagnose.ErrUnsupportedVersion) {
		t.Fatal("old policy silently accepted expanded verification semantics")
	}
}
