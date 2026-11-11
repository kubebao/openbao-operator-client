package adapter_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func TestBlueGreenRevisionProjection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		present bool
		value   any
		state   evidence.ValueState
	}{
		{"omitted", false, nil, evidence.ValueUnknown},
		{"valid", true, "blue-abc_123.v2", evidence.ValueReported},
		{"single character", true, "A", evidence.ValueReported},
		{"maximum length", true, strings.Repeat("a", 63), evidence.ValueReported},
		{"null", true, nil, evidence.ValueInvalid},
		{"empty", true, "", evidence.ValueInvalid},
		{"wrong type", true, map[string]any{"detail": "REVISION_CANARY"}, evidence.ValueInvalid},
		{"oversized", true, strings.Repeat("a", 64), evidence.ValueInvalid},
		{"invalid characters", true, "REVISION_CANARY/path", evidence.ValueInvalid},
		{"leading punctuation", true, "-blue", evidence.ValueInvalid},
		{"trailing punctuation", true, "blue_", evidence.ValueInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bluegreen := map[string]any{"phase": "Idle", "greenRevision": "green-123"}
			if tc.present {
				bluegreen["blueRevision"] = tc.value
			}
			cluster := projectBlueGreenFixture(t, bluegreen)
			if cluster.BlueGreen.BlueRevision.State != tc.state ||
				cluster.BlueGreen.GreenRevision.State != evidence.ValueReported || *cluster.BlueGreen.GreenRevision.Value != "green-123" {
				t.Fatal("revision projection discarded state or an independent revision")
			}
			assertBlueGreenReplay(t, cluster)
		})
	}
}

func projectBlueGreenFixture(t *testing.T, bluegreen any) evidence.Cluster {
	t.Helper()
	object := map[string]any{
		"apiVersion": "openbao.org/v1alpha1", "kind": "OpenBaoCluster",
		"metadata": map[string]any{"namespace": "team", "name": "bao", "uid": "cluster-1", "resourceVersion": "10", "generation": 4},
		"spec":     map[string]any{"replicas": 3},
		"status":   map[string]any{"blueGreen": bluegreen},
	}
	raw, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	cluster, err := adapter.ProjectCluster(raw, projectionOptions())
	if err != nil {
		t.Fatal(err)
	}
	return cluster
}

func assertBlueGreenReplay(t *testing.T, cluster evidence.Cluster) {
	t.Helper()
	raw, err := json.Marshal(cluster)
	if err != nil || bytes.Contains(raw, []byte("CANARY")) {
		t.Fatal("revision serialization retained invalid source data")
	}
	var replay evidence.Cluster
	if err := json.Unmarshal(raw, &replay); err != nil || replay.Validate() != nil {
		t.Fatal("valid revision evidence did not survive replay")
	}
	bad := "invalid/revision"
	cluster.BlueGreen.BlueRevision = evidence.Text{State: evidence.ValueReported, Value: &bad}
	if cluster.Validate() == nil {
		t.Fatal("replay validation accepted an invalid revision")
	}
}

func TestBlueGreenUnreportedObjectKeepsRevisionUnknown(t *testing.T) {
	t.Parallel()
	for _, value := range []any{nil, "REVISION_CANARY"} {
		cluster := projectBlueGreenFixture(t, value)
		if cluster.BlueGreen.BlueRevision.State != evidence.ValueUnknown || cluster.BlueGreen.GreenRevision.State != evidence.ValueUnknown {
			t.Fatal("unreported blue-green object acquired revision facts")
		}
	}
}
