package diagnose_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/kubebao/openbao-operator-client/diagnose"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func TestInvalidCaptureReportsIndexedPathWithoutValues(t *testing.T) {
	t.Parallel()
	i := workloadInput(t)
	prototype := i.Pods.Observations[0]
	i.Pods.Observations = nil
	for index := range 4 {
		pod := prototype
		pod.Metadata.Reference.Identity.Name = fmt.Sprintf("pod-%d", index)
		pod.Metadata.Reference.Identity.UID = fmt.Sprintf("uid-%d", index)
		i.Pods.Observations = append(i.Pods.Observations, pod)
	}
	i.Pods.Collection.Items = 4
	i.Pods.Observations[3].Metadata.Reference.Identity.UID = "CANARY_UID"
	i.Pods.Observations[3].Metadata.Reference.ResourceVersion = evidence.Text{State: evidence.ValueReported}
	_, err := diagnose.Analyze(capture(i), workloadPolicy(t))
	var invalid *evidence.ValidationError
	if !errors.Is(err, diagnose.ErrInvalidCapture) || !errors.Is(err, evidence.ErrInvalidEvidence) || !errors.As(err, &invalid) {
		t.Fatalf("validation lost its sentinel classifications: %v", err)
	}
	if invalid.Path != "clusters[0].pods.observations[3].metadata.reference.resourceVersion" || strings.Contains(err.Error(), "CANARY") {
		t.Fatalf("validation path contains data or omits the failing field: %v", err)
	}
}

func TestCoverageRetainsIndependentInsufficiencyCauses(t *testing.T) {
	t.Parallel()
	i := workloadInput(t)
	i.Pods.Collection.Outcome = evidence.CollectionPartial
	i.Pods.Observations[0].Conditions.Values[0].ObservedGeneration = evidence.Integer{State: evidence.ValueUnknown}
	other := readWorkloadFixture[evidence.Pod](t, "pod")
	other.Metadata.Reference.Identity.Name, other.Metadata.Reference.Identity.UID = "other", "other"
	other.Metadata.Owners = evidence.Owners{State: evidence.ValueUnknown}
	i.Pods.Observations = append(i.Pods.Observations, other)
	i.Pods.Collection.Items++
	_, coverage := runWorkloadRule(t, i, diagnose.RulePod)
	want := []diagnose.CoverageReason{diagnose.ReasonAssociation, diagnose.ReasonGeneration, diagnose.ReasonScopeIncomplete}
	slices.Sort(want)
	if coverage.State != diagnose.Insufficient || !slices.Equal(coverage.Reasons, want) {
		t.Fatalf("independent insufficiency causes were lost: %+v", coverage)
	}
}
