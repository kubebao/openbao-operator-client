package diagnose_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/diagnose"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func verification(t *testing.T, i diagnose.ClusterInput) evidence.TargetVerification {
	t.Helper()
	after := i.Observation.Reference
	read := after.CollectedAt.Value.Add(time.Second)
	after.CollectedAt = timestamp(read)
	value := evidence.TargetVerification{
		SchemaVersion: evidence.SchemaVersion, AdapterContract: i.AdapterContract, Before: i.Observation.Reference,
		BeforeGeneration: i.Observation.Generation, After: &after, AfterGeneration: i.Observation.Generation,
		State: evidence.VerificationVerified,
		Read: evidence.Collection{
			Scope: i.Collection.Scope, Outcome: evidence.CollectionComplete, Items: 1,
			Interval: evidence.Interval{StartedAt: read, CompletedAt: read},
		},
	}
	if value.Validate() != nil {
		t.Fatal("invalid verification setup")
	}
	return value
}

func TestCurrentFindingsRequireFreshEvidenceAndMatchingVerification(t *testing.T) {
	t.Parallel()
	i, p := input(t), policy(t)
	p.ReferenceTime = p.ReferenceTime.Add(10 * time.Minute)
	report, err := diagnose.Analyze(capture(i), p)
	if err != nil || len(report.Findings) != 0 || report.Coverage[0].Reason != diagnose.ReasonStaleEvidence {
		t.Fatal("stale evidence produced current findings")
	}
	for _, outcome := range []evidence.CollectionOutcome{evidence.CollectionDeleted, evidence.CollectionReplaced} {
		i = input(t)
		i.Collection.Outcome = outcome
		report, err = diagnose.Analyze(capture(i), policy(t))
		if err != nil || len(report.Findings) != 0 {
			t.Fatal("deleted or replaced evidence produced current findings")
		}
	}
	i, p = input(t), policy(t)
	v := verification(t, i)
	i.Verification = &v
	p.ReferenceTime = p.ReferenceTime.Add(time.Second)
	report, err = diagnose.Analyze(capture(i), p)
	if err != nil || len(report.Findings) != 2 {
		t.Fatal("matching verification hid supported facts")
	}
	v.AfterGeneration = integer(5)
	v.State = evidence.VerificationChanged
	report, err = diagnose.Analyze(capture(i), p)
	if err != nil || len(report.Findings) != 0 || report.Coverage[0].Reason != diagnose.ReasonVerification {
		t.Fatal("changed target became current findings")
	}
	v = verification(t, i)
	version := "different"
	v.Before.ResourceVersion = evidence.Text{State: evidence.ValueReported, Value: &version}
	if _, err := diagnose.Analyze(capture(i), p); !errors.Is(err, diagnose.ErrInvalidCapture) {
		t.Fatal("verification from another read was borrowed")
	}
}

func TestFindingIdentityAndReferencesRemainIndependent(t *testing.T) {
	t.Parallel()
	report, err := diagnose.Analyze(capture(input(t)), policy(t))
	if err != nil {
		t.Fatal(err)
	}
	finding := report.Findings[0]
	key, err := finding.Key()
	if err != nil {
		t.Fatal(err)
	}
	changed := finding
	changed.Severity, changed.Summary = diagnose.Info, "Different library-authored text"
	again, err := changed.Key()
	if err != nil || key != again {
		t.Fatal("presentation changed finding identity")
	}
	changed.Subject.UID = "replacement"
	again, err = changed.Key()
	if err != nil || key == again {
		t.Fatal("replacement inherited finding identity")
	}
	finding.References = append(finding.References, finding.References[0])
	later := finding.References[0]
	later.CollectedAt = timestamp(later.CollectedAt.Value.Add(time.Second))
	finding.References = append(finding.References, later)
	first, err := diagnose.CanonicalFindings([]diagnose.Finding{finding})
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(finding.References)
	second, err := diagnose.CanonicalFindings([]diagnose.Finding{finding})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Fatal("reference order changed serialized findings")
	}
	*finding.References[0].CollectedAt.Value = finding.References[0].CollectedAt.Value.Add(time.Hour)
	after, _ := json.Marshal(first)
	if string(a) != string(after) {
		t.Fatal("canonical references share mutable source timestamps")
	}
	_, err = diagnose.CanonicalFindings([]diagnose.Finding{report.Findings[0], report.Findings[0]})
	if !errors.Is(err, diagnose.ErrDuplicateFinding) {
		t.Fatal("ambiguous finding identities accepted")
	}
}

func TestCoverageDistinguishesDiscoveryGetAndListAbsence(t *testing.T) {
	t.Parallel()
	i := input(t)
	i.Collection.Items = 0
	for _, action := range []evidence.ReadAction{evidence.ReadGet, evidence.ReadDiscovery, evidence.ReadList} {
		read := i.Collection
		read.Scope.Action = action
		if action != evidence.ReadGet {
			read.Scope.Name = ""
		}
		got := diagnose.ResolveCoverage(diagnose.CoverageInput{
			Selected: true, Requested: true, Applicability: diagnose.Applies,
			Collection: read, EvidenceValid: true,
		})
		if got.State != diagnose.Evaluated || got.AbsenceConfirmed != (action == evidence.ReadList) {
			t.Fatal("non-list scope proved absence")
		}
	}
	i.Observation.WorkloadError = evidence.ControllerError{
		State:  evidence.ObjectReported,
		Reason: evidence.Text{State: evidence.ValueReported, Value: new("CANARY")}, At: evidence.Timestamp{State: evidence.ValueUnknown},
	}
	_, err := diagnose.Analyze(capture(i), policy(t))
	if !errors.Is(err, diagnose.ErrInvalidCapture) { // Zero items cannot describe the supplied observation.
		t.Fatal("incoherent complete read accepted")
	}
	i.Collection.Items = 1
	report, err := diagnose.Analyze(capture(i), policy(t))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(report)
	if err != nil || strings.Contains(string(encoded), "CANARY") {
		t.Fatal("unused producer fields escaped diagnostic text")
	}
}
