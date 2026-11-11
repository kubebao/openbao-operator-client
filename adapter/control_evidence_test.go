package adapter_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func TestClusterDeletionTimestampProjection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		present bool
		value   any
		state   evidence.ValueState
		instant string
	}{
		{name: "omitted", state: evidence.ValueUnknown},
		{name: "null", present: true, state: evidence.ValueInvalid},
		{name: "malformed", present: true, value: "DELETION_CANARY", state: evidence.ValueInvalid},
		{name: "wrong type", present: true, value: map[string]any{"detail": "DELETION_CANARY"}, state: evidence.ValueInvalid},
		{name: "empty", present: true, value: "", state: evidence.ValueInvalid},
		{name: "zero", present: true, value: "0001-01-01T00:00:00Z", state: evidence.ValueInvalid},
		{
			name: "reported", present: true, value: "2026-11-10T12:30:00.123456789+01:00", state: evidence.ValueReported,
			instant: "2026-11-10T11:30:00.123456789Z",
		},
		{
			name: "future remains a reported fact", present: true, value: "2026-11-11T12:00:00Z", state: evidence.ValueReported,
			instant: "2026-11-11T12:00:00Z",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var raw map[string]any
			decoder := json.NewDecoder(bytes.NewReader(clusterFixture(t)))
			decoder.UseNumber()
			if err := decoder.Decode(&raw); err != nil {
				t.Fatal(err)
			}
			metadata, ok := raw["metadata"].(map[string]any)
			if !ok {
				t.Fatal("fixture metadata is not an object")
			}
			if tc.present {
				metadata["deletionTimestamp"] = tc.value
			}
			input, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			got, err := adapter.ProjectCluster(input, projectionOptions())
			if err != nil {
				t.Fatal(err)
			}
			assertDeletionObservation(t, got, tc.state, tc.instant)
		})
	}
}

func assertDeletionObservation(t *testing.T, got evidence.Cluster, state evidence.ValueState, instant string) {
	t.Helper()
	if got.DeletionTimestamp.State != state {
		t.Fatalf("deletion timestamp state = %s, want %s", got.DeletionTimestamp.State, state)
	}
	if instant != "" {
		if got.DeletionTimestamp.Value == nil || got.DeletionTimestamp.Value.Format(time.RFC3339Nano) != instant {
			t.Fatalf("reported timestamp = %+v, want %s", got.DeletionTimestamp, instant)
		}
	} else if got.DeletionTimestamp.Value != nil {
		t.Fatal("unreported deletion timestamp retained a value")
	}
	if got.Validate() != nil || got.Reference.Identity.UID != "cluster-1" || got.Paused.State != evidence.ValueImplied {
		t.Fatal("deletion timestamp discarded independent cluster facts")
	}
	serialized, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(serialized, []byte("CANARY")) {
		t.Fatal("projection retained sensitive source data")
	}
	var replay evidence.Cluster
	if err := json.Unmarshal(serialized, &replay); err != nil {
		t.Fatal(err)
	}
	if replay.Validate() != nil || !reflect.DeepEqual(replay, got) {
		t.Fatal("serialized replay changed the deletion observation")
	}
}
