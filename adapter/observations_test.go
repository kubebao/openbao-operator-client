package adapter_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func TestSnapshotKeysUseObjectNameBounds(t *testing.T) {
	t.Parallel()
	for _, length := range []int{129, 254, 1024, 1025} {
		key := strings.Repeat("prefix/", length/7) + strings.Repeat("x", length%7)
		raw := bytes.Replace(operationalFixture(t), []byte("backups/bao.snap"), []byte(key), 1)
		got, err := adapter.ProjectCluster(raw, projectionOptions())
		if err != nil {
			t.Fatal(err)
		}
		if length <= 1024 {
			if got.Backup.SnapshotKey.State != evidence.ValueReported || *got.Backup.SnapshotKey.Value != key {
				t.Fatalf("valid %d-byte object key was discarded", length)
			}
		} else if got.Backup.SnapshotKey.State != evidence.ValueInvalid || got.Backup.SnapshotKey.Value != nil {
			t.Fatal("oversized object key retained source data")
		}
	}
}

func operationalFixture(t testing.TB) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/operational.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func restoreFixture(t testing.TB) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/restore.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestOperationalObservations(t *testing.T) {
	t.Parallel()
	got, err := adapter.ProjectCluster(operationalFixture(t), projectionOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got.Backup.Configuration != evidence.ObjectReported || got.Backup.State != evidence.ObjectReported ||
		*got.Backup.ConsecutiveFailures.Value != 2 || *got.Backup.SizeBytes.Value != 9007199254741007 ||
		got.Backup.LastSuccessAt.Value.Equal(*got.Backup.LastFailureAt.Value) {
		t.Fatal("backup facts were lost or conflated")
	}
	if *got.Backup.SnapshotKey.Value != "backups/bao.snap" ||
		*got.Backup.SnapshotDigest.Value != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatal("snapshot references were lost")
	}
	test := got.Backup.RestoreTest
	if test.Active != evidence.ObjectReported || *test.LastOutcome.Value != "Failed" || test.LastSuccessAt.Value == nil ||
		*test.Conditions.Values[0].Reason.Value != "SnapshotUnavailable" {
		t.Fatal("restore-test facts became a derived outcome")
	}
	if *got.Upgrade.ExecutionID.Value != "upgrade-1" || !reflect.DeepEqual(got.Upgrade.CompletedPods.Values, []int64{0, 2}) ||
		*got.Upgrade.Failure.Reason.Value != "Failed" {
		t.Fatal("upgrade observations were lost or nondeterministic")
	}
	if *got.BlueGreen.LastFailedJob.Value != "failed-job" || *got.ReadReplicas.Ready.Value != 1 ||
		*got.ReadReplicas.Desired.Value != 2 || !*got.Maintenance.Enabled.Value || !*got.BreakGlass.Active.Value {
		t.Fatal("operational and read-replica observations were lost")
	}
	if *got.RestoreHold.Value != "operation-1" || *got.RestoreOrigin.Value != "request-1" ||
		*got.OperationLock.Operation.Value != "Upgrade" || *got.LegacyRestore.UID.Value != "old-request" ||
		*got.WorkloadError.Reason.Value != "PolicyFailure" || *got.AdminOpsError.Reason.Value != "Failed" {
		t.Fatal("reported restriction or controller observations were lost")
	}
	assertSafeReplay(t, got)
}

func assertSafeReplay[T any](t *testing.T, value T) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	forbiddenFields := []string{"CANARY", "message", "nonce", "remoteReplica", "historyConflict", "rollbackReason", "highestAppliedIndex"}
	for _, forbidden := range forbiddenFields {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("unapproved data retained: %s", forbidden)
		}
	}
	var replay T
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&replay); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(value, replay) {
		t.Fatal("serialized observations changed facts")
	}
}

func TestObjectPresenceAndMissingCounters(t *testing.T) {
	t.Parallel()
	base := operationalFixture(t)
	for _, tc := range []struct {
		name  string
		raw   string
		want  evidence.ObjectState
		child evidence.ObjectState
	}{
		{"missing-parent", ``, evidence.ObjectUnknown, evidence.ObjectUnknown},
		{"absent", `{}`, evidence.ObjectAbsent, evidence.ObjectUnknown},
		{"null", `{"backup":null}`, evidence.ObjectNull, evidence.ObjectUnknown},
		{"empty", `{"backup":{}}`, evidence.ObjectReported, evidence.ObjectAbsent},
		{"invalid", `{"backup":[]}`, evidence.ObjectInvalid, evidence.ObjectUnknown},
		{"invalid-parent", `null`, evidence.ObjectInvalid, evidence.ObjectUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var object map[string]json.RawMessage
			if err := json.Unmarshal(base, &object); err != nil {
				t.Fatal(err)
			}
			if tc.raw == "" {
				delete(object, "status")
			} else {
				object["status"] = json.RawMessage(tc.raw)
			}
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			got, err := adapter.ProjectCluster(data, projectionOptions())
			if err != nil {
				t.Fatal(err)
			}
			if got.Backup.State != tc.want || got.Backup.ConsecutiveFailures.State != evidence.ValueUnknown ||
				got.Backup.RestoreTest.State != tc.child {
				t.Fatal("presence became a blanket zero/false or empty child")
			}
		})
	}
}

func TestOperationalFieldValidation(t *testing.T) {
	t.Parallel()
	base := operationalFixture(t)
	data := bytes.Replace(base, []byte(`"consecutiveFailures":2`), []byte(`"consecutiveFailures":-1`), 1)
	data = bytes.Replace(data, []byte(`"lastBackupSize":9007199254741007`), []byte(`"lastBackupSize":null`), 1)
	data = bytes.Replace(data, []byte(`"completedPods":[2,0]`), []byte(`"completedPods":[2,2]`), 1)
	data = bytes.Replace(data, []byte(`"phase":"Syncing"`), []byte(`"phase":"Unrecognized"`), 1)
	data = bytes.Replace(data, []byte(`"lastBackupName":"backups/bao.snap"`),
		[]byte(`"lastBackupName":"s3://user:CANARY@bucket/key?token=CANARY"`), 1)
	data = bytes.Replace(data, []byte("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), []byte("md5:CANARY"), 1)
	got, err := adapter.ProjectCluster(data, projectionOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got.Backup.ConsecutiveFailures.State != evidence.ValueInvalid || got.Backup.SizeBytes.State != evidence.ValueInvalid ||
		got.Upgrade.CompletedPods.State != evidence.ValueInvalid || got.BlueGreen.Phase.State != evidence.ValueInvalid ||
		got.Backup.SnapshotKey.State != evidence.ValueInvalid || got.Backup.SnapshotDigest.State != evidence.ValueInvalid ||
		got.Backup.LastSuccessAt.Value == nil {
		t.Fatal("malformed fields discarded independent facts or acquired defaults")
	}
	assertSafeReplay(t, got)
	options := projectionOptions()
	options.Limits.MaxEntries = 1
	got, err = adapter.ProjectCluster(base, options)
	if err != nil {
		t.Fatal(err)
	}
	if got.Upgrade.CompletedPods.State != evidence.ValueInvalid || got.Upgrade.ExecutionID.Value == nil {
		t.Fatal("ordinal limit lost independent upgrade facts")
	}
}

func TestRestoreObservationsStayIndependent(t *testing.T) {
	t.Parallel()
	got, err := adapter.ProjectRestore(restoreFixture(t), projectionOptions())
	if err != nil {
		t.Fatal(err)
	}
	if *got.Phase.Value != "Unknown" || *got.Execution.Stage.Value != "Committed" || got.SubmissionClaim != evidence.ObjectReported ||
		got.Target.AppliedAt.Value == nil || got.RestartCompletedAt.Value == nil || *got.Target.Cleanup.Value != "Failed" ||
		*got.Target.UID.Value != "cluster-1" || *got.Execution.TargetUID.Value != "cluster-1" {
		t.Fatal("submission, reported application, and restart became a single outcome")
	}
	assertSafeReplay(t, got)
	if got.Validate() != nil {
		t.Fatal("restore replay is not valid normalized evidence")
	}
	conflict := bytes.Replace(restoreFixture(t), []byte(`"targetUID":"cluster-1"`), []byte(`"targetUID":"other-uid"`), 1)
	got, err = adapter.ProjectRestore(conflict, projectionOptions())
	if err != nil {
		t.Fatal(err)
	}
	if *got.Target.UID.Value == *got.Execution.TargetUID.Value {
		t.Fatal("projection selected a matching UID and hid a conflict")
	}
}

func TestRestorePhaseDoesNotDependOnAge(t *testing.T) {
	t.Parallel()
	data := bytes.Replace(restoreFixture(t), []byte(`"phase":"Unknown",`), nil, 1)
	options := projectionOptions()
	first, err := adapter.ProjectRestore(data, options)
	if err != nil {
		t.Fatal(err)
	}
	options.CollectedAt = options.CollectedAt.AddDate(0, 1, 0)
	second, err := adapter.ProjectRestore(data, options)
	if err != nil {
		t.Fatal(err)
	}
	if first.Phase.State != evidence.ValueUnknown || second.Phase.State != evidence.ValueUnknown {
		t.Fatal("request age fabricated a reported phase")
	}
	data = bytes.Replace(data, []byte(`"stage":"Committed"`), []byte(`"stage":"RestoreSubmitted"`), 1)
	got, err := adapter.ProjectRestore(data, options)
	if err != nil {
		t.Fatal(err)
	}
	if got.Execution.Stage.State != evidence.ValueInvalid {
		t.Fatal("a non-API execution stage was accepted")
	}
}

func TestNativeRecoveryIsNotAnApprovedAPI(t *testing.T) {
	t.Parallel()
	data := bytes.Replace(restoreFixture(t), []byte("OpenBaoRestore"), []byte("OpenBaoRecovery"), 1)
	_, err := adapter.ProjectRestore(data, projectionOptions())
	assertProjectionError(t, err, adapter.ResourceMismatch)
}

func TestEvidenceSerializationContainsCurrentFieldsWithoutVersionGates(t *testing.T) {
	t.Parallel()
	cluster, err := adapter.ProjectCluster(operationalFixture(t), projectionOptions())
	if err != nil {
		t.Fatal(err)
	}
	restore, err := adapter.ProjectRestore(restoreFixture(t), projectionOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{cluster, restore} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, removed := range []string{"schemaVersion", "adapterContract", "restore-retention-v"} {
			if bytes.Contains(raw, []byte(removed)) {
				t.Fatalf("serialized evidence retained %s", removed)
			}
		}
	}
	cluster.Paused = evidence.Boolean{}
	restore.Target.UID = evidence.Text{}
	if cluster.Validate() == nil || restore.Validate() == nil {
		t.Fatal("required value states bypassed typed validation")
	}
}

func FuzzProjectRestore(f *testing.F) {
	f.Add(restoreFixture(f))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := adapter.ProjectRestore(data, projectionOptions())
		if err != nil {
			return
		}
		if got.Validate() != nil {
			t.Fatal("invalid normalized restore evidence")
		}
		if _, err := json.Marshal(got); err != nil {
			t.Fatal("restore evidence cannot be serialized")
		}
	})
}

func TestBackupFailureCountIsImpliedOnlyAfterASuccessfulLatestAttempt(t *testing.T) {
	t.Parallel()
	base := bytes.Replace(operationalFixture(t), []byte(`"consecutiveFailures":2,`), nil, 1)
	attempt := []byte(`"lastAttemptTime":"2026-11-10T11:00:00Z",`)
	for _, tc := range []struct {
		name    string
		attempt string
		want    evidence.ValueState
	}{
		{"attempt after success", string(attempt), evidence.ValueUnknown},
		{"success after attempt", `"lastAttemptTime":"2026-11-10T09:59:50Z",`, evidence.ValueImplied},
		{"success at attempt", `"lastAttemptTime":"2026-11-10T10:00:00Z",`, evidence.ValueImplied},
		{"no recorded attempt", ``, evidence.ValueUnknown},
		{"malformed attempt", `"lastAttemptTime":"yesterday",`, evidence.ValueUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := bytes.Replace(base, attempt, []byte(tc.attempt), 1)
			got, err := adapter.ProjectCluster(data, projectionOptions())
			if err != nil {
				t.Fatal(err)
			}
			count := got.Backup.ConsecutiveFailures
			if count.State != tc.want || tc.want == evidence.ValueImplied && *count.Value != 0 {
				t.Fatalf("consecutive failures = %+v, want state %s", count, tc.want)
			}
		})
	}
	got, err := adapter.ProjectCluster(bytes.Replace(operationalFixture(t), attempt,
		[]byte(`"lastAttemptTime":"2026-11-10T09:00:00Z",`), 1), projectionOptions())
	if err != nil || got.Backup.ConsecutiveFailures.State != evidence.ValueReported || *got.Backup.ConsecutiveFailures.Value != 2 {
		t.Fatal("a reported failure count was replaced by an inference")
	}
}
