package diagnose_test

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/diagnose"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func restoreGroupInput(t testing.TB, native bool) (diagnose.ClusterInput, diagnose.Policy) {
	t.Helper()
	i, p := restoreInput(t), restorePolicy(t)
	if !native {
		return i, p
	}
	const contract = "kubebao.org/v1alpha1@22894cdc/restore-retention-v1"
	i.AdapterContract, i.Observation.AdapterContract, i.Restores.AdapterContract = contract, contract, contract
	i.Verification.AdapterContract = contract
	for _, ref := range []*evidence.Reference{
		&i.Observation.Reference, &i.Verification.Before, i.Verification.After, &i.Restores.Observations[0].Reference,
	} {
		ref.Identity.Group, ref.APIVersion = "kubebao.org", "kubebao.org/v1alpha1"
	}
	i.Subject = i.Observation.Reference.Identity
	i.Collection.Scope.Group = "kubebao.org"
	i.Restores.Collection.Scope.Group, i.Verification.Read.Scope.Group = "kubebao.org", "kubebao.org"
	i.Restores.Observations[0].AdapterContract = contract
	p.ApprovedContracts, p.RestoreUIDBindingContracts = []string{contract}, []string{contract}
	return i, p
}

func TestRestoreDisplaySelectionDoesNotReplaceEarlierHeldConcern(t *testing.T) {
	t.Parallel()
	for _, native := range []bool{true, false} {
		i, p := restoreGroupInput(t, native)
		t.Run(i.Subject.Group, func(t *testing.T) {
			p.ReportHistoricalRestores = false
			held := i.Restores.Observations[0]
			held.Phase, held.Execution.Stage, held.Target.Cleanup = text("Failed"), text("TerminalObserved"), text("Complete")
			i.Observation.RestoreHold, i.Observation.RestoreOrigin = text(
				held.Reference.Identity.UID,
			), evidence.Text{
				State: evidence.ValueUnknown,
			}
			latest := held
			latest.Reference.Identity.Name, latest.Reference.Identity.UID = "latest", "request-latest"
			latest.CreatedAt = timestamp(held.CreatedAt.Value.Add(time.Hour))
			latest.Phase, latest.Execution.Stage, latest.Execution.OperationID = text(
				"Completed",
			), text(
				"FollowThroughComplete",
			), text(
				"operation-latest",
			)
			foreign := latest
			foreign.Reference.Identity.Name, foreign.Reference.Identity.UID = "foreign", "request-foreign"
			foreign.CreatedAt = timestamp(latest.CreatedAt.Value.Add(time.Hour))
			foreign.TargetName, foreign.Target.UID, foreign.Execution.TargetUID = text(
				"other",
			), text(
				"other-cluster",
			), text(
				"other-cluster",
			)
			candidate := foreign
			candidate.Reference.Identity.Name, candidate.Reference.Identity.UID = "candidate", "request-candidate"
			candidate.CreatedAt = timestamp(foreign.CreatedAt.Value.Add(30 * time.Minute))
			candidate.TargetName = text(i.Subject.Name)
			candidate.Target.UID, candidate.Execution.TargetUID = evidence.Text{
				State: evidence.ValueUnknown,
			}, evidence.Text{
				State: evidence.ValueUnknown,
			}
			i.Restores.Observations = []evidence.Restore{candidate, latest, foreign, held}
			i.Restores.Collection.Items = len(i.Restores.Observations)
			display, found, err := diagnose.LatestRestore(
				*i.Observation,
				i.Restores.Observations,
				i.AdapterContract,
				i.Observation.Reference.APIVersion,
			)
			if err != nil || !found || display.Reference.Identity != latest.Reference.Identity {
				t.Fatal("newer foreign or name-only request displaced the latest UID-confirmed display record")
			}
			first, coverage := runRestore(t, i, p)
			if coverage.State != diagnose.Insufficient || coverage.Reason != diagnose.ReasonAssociation || len(first.Findings) != 1 ||
				first.Findings[0].Severity != diagnose.Error || first.Findings[0].Subject != held.Reference.Identity {
				t.Fatal("display selection or unresolved candidate hid the earlier independently confirmed held failure")
			}
			encoded, err := json.Marshal(capture(i))
			if err != nil {
				t.Fatal(err)
			}
			var replay diagnose.Capture
			if err := json.Unmarshal(encoded, &replay); err != nil {
				t.Fatal(err)
			}
			slices.Reverse(replay.Clusters[0].Restores.Observations)
			second, err := diagnose.Analyze(replay, p)
			if err != nil {
				t.Fatal(err)
			}
			a, err := json.Marshal(first)
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(second)
			if err != nil || !bytes.Equal(a, b) {
				t.Fatal("nonempty restore replay changed findings, coverage, or order")
			}
		})
	}
}

func TestRestoreDiagnosisKeepsIdenticalUIDsInDifferentAPIGroupsSeparate(t *testing.T) {
	t.Parallel()
	legacy, p := restoreGroupInput(t, false)
	native, _ := restoreGroupInput(t, true)
	for _, i := range []*diagnose.ClusterInput{&legacy, &native} {
		i.Observation.RestoreHold = text(i.Restores.Observations[0].Reference.Identity.UID)
	}
	p.ApprovedContracts = []string{legacy.AdapterContract, native.AdapterContract}
	p.RestoreUIDBindingContracts = slices.Clone(p.ApprovedContracts)
	p.ReportHistoricalRestores = false
	report, err := diagnose.Analyze(capture(legacy, native), p)
	if err != nil || len(report.Findings) != 2 {
		t.Fatalf("same UIDs in different groups lost an independent restore finding: %v", err)
	}
	groups := map[string]bool{}
	for _, finding := range report.Findings {
		if finding.Subject.UID != "request-1" || finding.Severity != diagnose.Error || finding.Subject.Origin != legacy.Subject.Origin {
			t.Fatal("restore identity or held severity changed across groups")
		}
		groups[finding.Subject.Group] = true
		for _, ref := range finding.References {
			if ref.Identity.Group != finding.Subject.Group {
				t.Fatal("restore finding references crossed the selected API group")
			}
		}
	}
	if !groups["openbao.org"] || !groups["kubebao.org"] {
		t.Fatal("same UIDs merged native and legacy restore subjects")
	}
}
