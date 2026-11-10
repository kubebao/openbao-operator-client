package evidence_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/kubebao/openbao-operator-client/evidence"
)

func TestGenerationContract(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/generations.json")
	if err != nil {
		t.Fatal(err)
	}
	var scenarios []struct {
		Name      string                   `json:"name"`
		Resource  *int64                   `json:"resource"`
		Status    *int64                   `json:"status"`
		Condition *int64                   `json:"condition"`
		Want      evidence.GenerationState `json:"want"`
		WantCond  evidence.GenerationState `json:"wantCondition"`
	}
	if err := json.Unmarshal(data, &scenarios); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			t.Parallel()
			if got := evidence.CompareGeneration(scenario.Resource, scenario.Status); got != scenario.Want {
				t.Fatalf("resource acknowledgement = %s, want %s", got, scenario.Want)
			}
			if got := evidence.CompareGeneration(scenario.Resource, scenario.Condition); got != scenario.WantCond {
				t.Fatalf("condition acknowledgement = %s, want %s", got, scenario.WantCond)
			}
		})
	}
}
