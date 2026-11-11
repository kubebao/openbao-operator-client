package contracttest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kubebao/openbao-operator-client/adapter"
)

type safeProjection struct {
	Ready          number      `json:"ready"`
	Conditions     []condition `json:"conditions"`
	ConditionState string      `json:"conditionState"`
	ContainerCode  *int64      `json:"containerExitCode"`
}

func project(t *testing.T, raw map[string]any, contract string) safeProjection {
	result := safeProjection{Ready: readiness(t, raw, contract)}
	result.Conditions, result.ConditionState = conditions(t, raw)
	result.ContainerCode = projectedContainerCode(t, raw)
	return result
}

// The fixture scalar is placed in its public Pod field before production projection.
func projectedContainerCode(t *testing.T, raw map[string]any) *int64 {
	t.Helper()
	code, state := field(raw, "status", "containerExitCode")
	if state != "present" {
		return nil
	}
	object := map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"namespace": "team", "name": "bao-0", "uid": "pod-1"},
		"status": map[string]any{"containerStatuses": []any{map[string]any{
			"name": "bao", "state": map[string]any{"terminated": map[string]any{"exitCode": code}},
		}}},
	}
	data, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	value, err := adapter.ProjectPod(data, fixtureOptions("origin", adapter.KubernetesCoreV1))
	if err != nil || len(value.Containers.Values) != 1 {
		t.Fatal("container fixture could not be projected")
	}
	return value.Containers.Values[0].ExitCode.Value
}

func TestSafeProjection(t *testing.T) {
	t.Parallel()
	cases := fixtures[[]struct {
		Name     string         `json:"name"`
		Raw      map[string]any `json:"raw"`
		Contract string         `json:"contract"`
		Canaries []string       `json:"canaries"`
		Want     safeProjection `json:"want"`
	}](t, "projection")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			got := project(t, tc.Raw, tc.Contract)
			equal(t, got, tc.Want)
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			full, err := json.Marshal(projectFixture(t, tc.Raw, tc.Contract))
			if err != nil {
				t.Fatal(err)
			}
			for _, canary := range tc.Canaries {
				if strings.Contains(string(encoded), canary) || strings.Contains(string(full), canary) {
					t.Fatalf("provider text retained: %s", canary)
				}
			}
			var fields map[string]any
			if err := json.Unmarshal(full, &fields); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"exitCode", "resultCode", "failureThreshold"} {
				if _, exists := fields[key]; exists {
					t.Fatalf("process policy in evidence: %s", key)
				}
			}
		})
	}
}
