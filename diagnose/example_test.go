package diagnose_test

import (
	"fmt"
	"time"

	"github.com/kubebao/openbao-operator-client/diagnose"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func ExampleAnalyze() {
	at := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	unknownText := evidence.Text{State: evidence.ValueUnknown}
	unknownTime := evidence.Timestamp{State: evidence.ValueUnknown}
	cluster := evidence.Cluster{
		Reference: evidence.Reference{
			Identity: evidence.Identity{
				Origin:    "cluster-a",
				Group:     "openbao.org",
				Kind:      "OpenBaoCluster",
				Namespace: "team",
				Name:      "bao",
				UID:       "cluster-1",
			},
			APIVersion:      "openbao.org/v1alpha1",
			ResourceVersion: evidence.Text{State: evidence.ValueReported, Value: new("41")},
			CollectedAt:     evidence.Timestamp{State: evidence.ValueReported, Value: &at},
		},
		CreatedAt:          evidence.Timestamp{State: evidence.ValueReported, Value: new(at.Add(-time.Hour))},
		DeletionTimestamp:  unknownTime,
		Generation:         evidence.Integer{State: evidence.ValueReported, Value: new(int64(4))},
		ObservedGeneration: evidence.Integer{State: evidence.ValueReported, Value: new(int64(4))},
		DesiredVersion:     unknownText,
		ObservedVersion:    unknownText,
		Phase:              unknownText,
		DesiredReplicas:    evidence.Integer{State: evidence.ValueReported, Value: new(int64(3))},
		ReadyReplicas:      evidence.Integer{State: evidence.ValueReported, Value: new(int64(0))},
		Paused:             evidence.Boolean{State: evidence.ValueReported, Value: new(false)},
		Conditions:         evidence.Conditions{State: evidence.ValueReported, Values: []evidence.Condition{}},
		Backup: evidence.Backup{
			Configuration:       evidence.ObjectAbsent,
			State:               evidence.ObjectUnknown,
			LastSuccessAt:       unknownTime,
			LastAttemptAt:       unknownTime,
			NextScheduledAt:     unknownTime,
			ConsecutiveFailures: evidence.Integer{State: evidence.ValueUnknown},
			LastFailureReason:   unknownText,
			LastFailureAt:       unknownTime,
			SizeBytes:           evidence.Integer{State: evidence.ValueUnknown},
			SnapshotKey:         unknownText,
			SnapshotDigest:      unknownText,
			RestoreTest: evidence.RestoreTest{
				State:          evidence.ObjectUnknown,
				Active:         evidence.ObjectUnknown,
				Last:           evidence.ObjectUnknown,
				LastOutcome:    unknownText,
				LastReason:     unknownText,
				LastSuccessAt:  unknownTime,
				FinishedAt:     unknownTime,
				SnapshotKey:    unknownText,
				SnapshotDigest: unknownText,
				Conditions:     evidence.Conditions{State: evidence.ValueUnknown},
			},
		},
		Upgrade: evidence.Upgrade{
			State:            evidence.ObjectAbsent,
			ExecutionID:      unknownText,
			TargetVersion:    unknownText,
			FromVersion:      unknownText,
			StartedAt:        unknownTime,
			CurrentPartition: evidence.Integer{State: evidence.ValueUnknown},
			CompletedPods:    evidence.Ordinals{State: evidence.ValueUnknown},
			Failure:          evidence.ControllerError{State: evidence.ObjectUnknown, Reason: unknownText, At: unknownTime},
		},
		BlueGreen: evidence.BlueGreen{
			State:           evidence.ObjectAbsent,
			Phase:           unknownText,
			OperationID:     unknownText,
			BlueRevision:    unknownText,
			GreenRevision:   unknownText,
			GreenVersion:    unknownText,
			StartedAt:       unknownTime,
			JobFailureCount: evidence.Integer{State: evidence.ValueUnknown},
			LastFailedJob:   unknownText,
		},
		ReadReplicas: evidence.ReplicaCounts{
			Configuration: evidence.ObjectAbsent,
			State:         evidence.ObjectAbsent,
			Desired:       evidence.Integer{State: evidence.ValueUnknown},
			Ready:         evidence.Integer{State: evidence.ValueUnknown},
		},
		Runtime:                 evidence.Runtime{State: evidence.ObjectAbsent, RestartAt: unknownText},
		UpgradeStrategy:         unknownText,
		AcceptedUpgradeStrategy: unknownText,
		Maintenance: evidence.Maintenance{
			State:   evidence.ObjectAbsent,
			Enabled: evidence.Boolean{State: evidence.ValueUnknown},
		},
		RestoreOrigin: unknownText,
		RestoreHold:   unknownText,
		OperationLock: evidence.OperationLock{
			State:      evidence.ObjectAbsent,
			Operation:  unknownText,
			Holder:     unknownText,
			AcquiredAt: unknownTime,
			RenewedAt:  unknownTime,
		},
		BreakGlass: evidence.BreakGlass{
			State:     evidence.ObjectAbsent,
			Active:    evidence.Boolean{State: evidence.ValueUnknown},
			Reason:    unknownText,
			EnteredAt: unknownTime,
		},
		LegacyRestore: evidence.LegacyRestore{
			State:              evidence.ObjectAbsent,
			Name:               unknownText,
			UID:                unknownText,
			RestartCompletedAt: unknownTime,
		},
		WorkloadError: evidence.ControllerError{State: evidence.ObjectAbsent, Reason: unknownText, At: unknownTime},
		AdminOpsError: evidence.ControllerError{State: evidence.ObjectAbsent, Reason: unknownText, At: unknownTime},
	}
	input := diagnose.ClusterInput{
		Subject:     cluster.Reference.Identity,
		Observation: &cluster,
		Collection: evidence.Collection{
			Scope: evidence.Scope{
				Origin:    "cluster-a",
				Group:     "openbao.org",
				Version:   "v1alpha1",
				Resource:  "openbaoclusters",
				Namespace: "team",
				Name:      "bao",
				Action:    evidence.ReadGet,
			},
			Interval: evidence.Interval{StartedAt: at, CompletedAt: at},
			Outcome:  evidence.CollectionComplete,
			Items:    1,
		},
	}
	policy := diagnose.DefaultPolicy(at)
	policy.Rules = []diagnose.RuleID{diagnose.RuleReadiness, diagnose.RuleBackupAge}

	report, err := diagnose.Analyze(diagnose.Capture{Clusters: []diagnose.ClusterInput{input}}, policy)
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, finding := range report.Findings {
		fmt.Println(finding.Rule, finding.Severity)
	}
	for _, coverage := range report.Coverage {
		if coverage.State != diagnose.CoverageExcluded {
			fmt.Println(coverage.Rule, coverage.State, coverage.Reason)
		}
	}
	// Output:
	// KBAO-CLUSTER-003 critical
	// KBAO-BACKUP-003 not_applicable not_configured
	// KBAO-CLUSTER-003 evaluated none
}

func ExampleResolveCoverage() {
	at := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	input := diagnose.CoverageInput{
		Selected:      true,
		Requested:     true,
		Applicability: diagnose.ApplicabilityApplies,
		EvidenceValid: true,
		Collection: evidence.Collection{
			Scope: evidence.Scope{
				Origin:    "cluster-a",
				Group:     "openbao.org",
				Version:   "v1alpha1",
				Resource:  "openbaorestores",
				Namespace: "team",
				Action:    evidence.ReadList,
			},
			Interval: evidence.Interval{StartedAt: at, CompletedAt: at},
			Outcome:  evidence.CollectionComplete,
		},
	}
	complete := diagnose.ResolveCoverage(input)
	fmt.Println(complete.State, complete.AbsenceConfirmed)

	input.Collection.Outcome = evidence.CollectionPartial
	partial := diagnose.ResolveCoverage(input)
	fmt.Println(partial.State, partial.AbsenceConfirmed)
	// Output:
	// evaluated true
	// insufficient false
}
