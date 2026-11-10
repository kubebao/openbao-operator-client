package adapter

import (
	"encoding/json"
	"math"
	"regexp"
	"slices"

	"github.com/kubebao/openbao-operator-client/evidence"
)

var snapshotDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func objectAt(raw map[string]any, path ...string) (map[string]any, evidence.ObjectState) {
	var cursor any = raw
	for i, key := range path {
		parent, ok := cursor.(map[string]any)
		if !ok {
			return nil, evidence.ObjectInvalid
		}
		value, found := parent[key]
		if !found {
			if i == len(path)-1 {
				return nil, evidence.ObjectAbsent
			}
			return nil, evidence.ObjectUnknown
		}
		cursor = value
	}
	if cursor == nil {
		return nil, evidence.ObjectNull
	}
	if object, ok := cursor.(map[string]any); ok {
		return object, evidence.ObjectReported
	}
	return nil, evidence.ObjectInvalid
}

func childObject(parent evidence.ObjectState, raw map[string]any, path ...string) (map[string]any, evidence.ObjectState) {
	if parent != evidence.ObjectReported {
		return nil, evidence.ObjectUnknown
	}
	return objectAt(raw, path...)
}

func enumText(raw map[string]any, allowed []string, path ...string) evidence.Text {
	value := text(raw, true, path...)
	if value.Value != nil && !slices.Contains(allowed, *value.Value) {
		return evidence.Text{State: evidence.ValueInvalid}
	}
	return value
}

func digest(raw map[string]any, path ...string) evidence.Text {
	value := text(raw, false, path...)
	if value.Value != nil && !snapshotDigest.MatchString(*value.Value) {
		return evidence.Text{State: evidence.ValueInvalid}
	}
	return value
}

func sizeBytes(raw map[string]any, path ...string) evidence.Integer {
	value := integer(raw, path...)
	if value.Value != nil && *value.Value < 0 {
		return evidence.Integer{State: evidence.ValueInvalid}
	}
	return value
}

func ordinals(raw map[string]any, limit int, path ...string) evidence.Ordinals {
	value, state := field(raw, path...)
	if state != evidence.ValueReported {
		return evidence.Ordinals{State: state}
	}
	items, ok := value.([]any)
	if !ok || len(items) > limit {
		return evidence.Ordinals{State: evidence.ValueInvalid}
	}
	result := evidence.Ordinals{State: evidence.ValueReported, Values: []int64{}}
	seen := map[int64]bool{}
	for _, item := range items {
		number, ok := item.(json.Number)
		if !ok {
			return evidence.Ordinals{State: evidence.ValueInvalid}
		}
		parsed, err := number.Int64()
		if err != nil || parsed < 0 || parsed > math.MaxInt32 || seen[parsed] {
			return evidence.Ordinals{State: evidence.ValueInvalid}
		}
		seen[parsed] = true
		result.Values = append(result.Values, parsed)
	}
	slices.Sort(result.Values)
	return result
}

func projectControllerError(raw map[string]any, state evidence.ObjectState) evidence.ControllerError {
	return evidence.ControllerError{State: state, Reason: text(raw, true, "reason"), At: timestamp(raw, "at")}
}

func projectRestoreTest(raw map[string]any, state evidence.ObjectState, limit int) evidence.RestoreTest {
	_, active := childObject(state, raw, "active")
	last, lastState := childObject(state, raw, "last")
	return evidence.RestoreTest{
		State: state, Active: active, Last: lastState,
		LastOutcome: enumText(last, []string{"Passed", "Failed"}, "outcome"), LastReason: text(last, true, "reason"),
		LastSuccessAt: timestamp(raw, "lastSuccessTime"), FinishedAt: timestamp(last, "finishedAt"),
		SnapshotKey: text(last, true, "key"), SnapshotDigest: digest(last, "digest"),
		Conditions: projectConditions(raw, limit, "conditions"),
	}
}

func projectBackup(root map[string]any, limit int) evidence.Backup {
	_, configuration := objectAt(root, "spec", "backup")
	raw, state := objectAt(root, "status", "backup")
	test, testState := childObject(state, raw, "restoreTest")
	return evidence.Backup{
		Configuration: configuration,
		State:         state,
		LastSuccessAt: timestamp(
			raw,
			"lastBackupTime",
		),
		LastAttemptAt:       timestamp(raw, "lastAttemptTime"),
		NextScheduledAt:     timestamp(raw, "nextScheduledBackup"),
		ConsecutiveFailures: count(raw, "consecutiveFailures"),
		LastFailureReason:   text(raw, true, "lastFailureReason"),
		LastFailureAt:       timestamp(raw, "lastFailureTime"),
		SizeBytes:           sizeBytes(raw, "lastBackupSize"),
		SnapshotKey:         text(raw, true, "lastBackupName"),
		SnapshotDigest:      digest(raw, "latestSnapshot", "digest"),
		RestoreTest:         projectRestoreTest(test, testState, limit),
	}
}

func projectUpgrade(root map[string]any, limit int) evidence.Upgrade {
	raw, state := objectAt(root, "status", "upgrade")
	failure, failureState := childObject(state, raw, "failure")
	return evidence.Upgrade{
		State:         state,
		ExecutionID:   text(raw, true, "executionID"),
		TargetVersion: version(raw, "targetVersion"),
		FromVersion:   version(raw, "fromVersion"),
		StartedAt: timestamp(
			raw,
			"startedAt",
		),
		CurrentPartition: count(raw, "currentPartition"),
		CompletedPods:    ordinals(raw, limit, "completedPods"),
		Failure:          projectControllerError(failure, failureState),
	}
}

func projectBlueGreen(root map[string]any) evidence.BlueGreen {
	raw, state := objectAt(root, "status", "blueGreen")
	return evidence.BlueGreen{
		State: state, Phase: enumText(raw, []string{
			"Idle", "DeployingGreen", "JoiningMesh", "Syncing", "Promoting", "DemotingBlue", "Cleanup",
			"RestoringReadReplicas", "RollingBack", "RollbackCleanup",
		}, "phase"),
		OperationID: text(raw, true, "operationID"), GreenVersion: version(raw, "greenVersion"), StartedAt: timestamp(raw, "startTime"),
		JobFailureCount: count(raw, "jobFailureCount"), LastFailedJob: text(raw, true, "lastJobFailure"),
	}
}

func projectOperationLock(root map[string]any) evidence.OperationLock {
	raw, state := objectAt(root, "status", "operationLock")
	return evidence.OperationLock{
		State: state, Operation: enumText(raw, []string{"Upgrade", "Backup", "Restore"}, "operation"), Holder: text(raw, true, "holder"),
		AcquiredAt: timestamp(raw, "acquiredAt"), RenewedAt: timestamp(raw, "renewedAt"),
	}
}

func projectBreakGlass(root map[string]any) evidence.BreakGlass {
	raw, state := objectAt(root, "status", "breakGlass")
	return evidence.BreakGlass{
		State:  state,
		Active: boolean(raw, "active"),
		Reason: enumText(
			raw,
			[]string{"RollbackConsensusRepairFailed", "RollbackCleanupPeerRemovalFailed"},
			"reason",
		),
		EnteredAt: timestamp(raw, "enteredAt"),
	}
}

func projectOperationalFields(root map[string]any, result *evidence.Cluster, limits ProjectionLimits) {
	result.Backup = projectBackup(root, limits.MaxConditions)
	result.Upgrade = projectUpgrade(root, limits.MaxEntries)
	result.BlueGreen = projectBlueGreen(root)
	result.OperationLock = projectOperationLock(root)
	result.BreakGlass = projectBreakGlass(root)
	readSpec, readConfiguration := objectAt(root, "spec", "readReplicas")
	readStatus, readState := objectAt(root, "status", "readReplicas")
	result.ReadReplicas = evidence.ReplicaCounts{
		Configuration: readConfiguration, State: readState,
		Desired: count(readSpec, "replicas"), Ready: count(readStatus, "readyReplicas"),
	}
	maintenance, maintenanceState := objectAt(root, "spec", "maintenance")
	result.Maintenance = evidence.Maintenance{State: maintenanceState, Enabled: boolean(maintenance, "enabled")}
	result.RestoreOrigin = text(root, true, "metadata", "annotations", "openbao.org/restore-origin")
	result.RestoreHold = text(root, true, "metadata", "annotations", "openbao.org/restore-hold")
	legacy, legacyState := objectAt(root, "status", "restore")
	result.LegacyRestore = evidence.LegacyRestore{
		State:              legacyState,
		Name:               text(legacy, true, "name"),
		UID:                text(legacy, true, "uid"),
		RestartCompletedAt: timestamp(legacy, "restartCompletedAt"),
	}
	workload, workloadState := objectAt(root, "status", "workload", "lastError")
	admin, adminState := objectAt(root, "status", "adminOps", "lastError")
	result.WorkloadError = projectControllerError(workload, workloadState)
	result.AdminOpsError = projectControllerError(admin, adminState)
}
