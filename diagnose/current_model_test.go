package diagnose_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/kubebao/openbao-operator-client/diagnose"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func TestCurrentTypedModelSerializesWithoutVersionOrProvenanceGates(t *testing.T) {
	t.Parallel()
	input, policy := restoreGroupInput(t, true)
	workloads := workloadInput(t)
	capture := capture(input)
	report, err := diagnose.Analyze(capture, policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{
		input.Observation, input.Restores.Observations[0], input.Verification,
		workloads.StatefulSets.Observations[0], workloads.Pods.Observations[0], capture, policy, report,
	} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, removed := range []string{
			"schemaVersion", "adapterContract", "ruleSetVersion", "approvedContracts", "restoreUIDBindingContracts",
		} {
			if bytes.Contains(data, []byte(removed)) {
				t.Fatalf("serialized %T retains %s", value, removed)
			}
		}
	}
	data, err := json.Marshal(capture)
	if err != nil {
		t.Fatal(err)
	}
	var replay diagnose.Capture
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&replay); err != nil {
		t.Fatal(err)
	}
	actual, err := diagnose.Analyze(replay, policy)
	if err != nil {
		t.Fatal(err)
	}
	first, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(actual)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("current model replay changed deterministic diagnostics")
	}
	replay.Clusters[0].Observation.Paused = evidence.Boolean{}
	if _, err := diagnose.Analyze(replay, policy); err == nil {
		t.Fatal("serialized malformed value state accepted")
	}
}
