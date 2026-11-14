package evidence_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/evidence"
)

func poolMetadata() evidence.WorkloadMetadata {
	at := time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)
	return evidence.WorkloadMetadata{
		RestartAt: evidence.Text{State: evidence.ValueUnknown}, ControllerRevision: evidence.Text{State: evidence.ValueUnknown},
		Reference: evidence.Reference{
			Identity:   evidence.Identity{Origin: "test", Group: "", Kind: "Pod", Namespace: "team", Name: "bao-0", UID: "pod-1"},
			APIVersion: "v1", ResourceVersion: evidence.Text{State: evidence.ValueUnknown},
			CollectedAt: evidence.Timestamp{State: evidence.ValueReported, Value: &at},
		},
		Owners: evidence.Owners{State: evidence.ValueUnknown}, ClusterOwnerUID: evidence.Text{State: evidence.ValueUnknown},
		Maintenance: evidence.Text{State: evidence.ValueUnknown}, Pool: evidence.Text{State: evidence.ValueUnknown},
		Revision: evidence.Text{State: evidence.ValueUnknown}, DeletionTimestamp: evidence.Timestamp{State: evidence.ValueUnknown},
	}
}

func poolText(value string) evidence.Text {
	return evidence.Text{State: evidence.ValueReported, Value: &value}
}

func TestWorkloadPoolAndRevisionValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, pool, revision string
		valid                bool
	}{
		{"voter", "voter", "blue-1", true},
		{"reader", "read-replica", "G.1_2-3", true},
		{"one character", "voter", "A", true},
		{"maximum length", "voter", strings.Repeat("a", 63), true},
		{"unsupported pool", "other", "blue", false},
		{"empty pool", "", "blue", false},
		{"empty revision", "voter", "", false},
		{"overlong revision", "voter", strings.Repeat("a", 64), false},
		{"leading punctuation", "voter", "-blue", false},
		{"trailing punctuation", "voter", "blue_", false},
		{"path separator", "voter", "blue/green", false},
		{"unicode", "voter", "blué", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			value := poolMetadata()
			value.Pool, value.Revision = poolText(tc.pool), poolText(tc.revision)
			if (value.Validate() == nil) != tc.valid {
				t.Fatal("metadata validation changed pool or revision constraints")
			}
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var replay evidence.WorkloadMetadata
			if err := json.Unmarshal(raw, &replay); err != nil || !reflect.DeepEqual(replay, value) {
				t.Fatal("metadata replay changed reported facts")
			}
			if (replay.Validate() == nil) != tc.valid {
				t.Fatal("replay accepted forged pool or revision evidence")
			}
		})
	}
}

func TestWorkloadGroupingValueStates(t *testing.T) {
	t.Parallel()
	for _, state := range []evidence.ValueState{evidence.ValueUnknown, evidence.ValueInvalid} {
		value := poolMetadata()
		value.Pool, value.Revision = evidence.Text{State: state}, evidence.Text{State: state}
		if value.Validate() != nil {
			t.Fatalf("unreported grouping state %s was rejected", state)
		}
	}
	for _, field := range []string{"pool", "revision"} {
		value := poolMetadata()
		bad := evidence.Text{State: evidence.ValueImplied, Value: new(string)}
		if field == "pool" {
			value.Pool = bad
		} else {
			value.Revision = bad
		}
		if value.Validate() == nil {
			t.Fatalf("an implied %s was accepted", field)
		}
	}
}
