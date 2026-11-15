package operations_test

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/operations"
)

func ExamplePreparePause() {
	raw := []byte(`{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster",` +
		`"metadata":{"namespace":"team","name":"bao","uid":"cluster-1","resourceVersion":"42","generation":4,` +
		`"creationTimestamp":"2026-11-10T09:00:00Z"},` +
		`"spec":{"replicas":3,"paused":false},"status":{"observedGeneration":4}}`)
	cluster, err := adapter.ProjectCluster(raw, adapter.ProjectionOptions{
		Origin:      "cluster-a",
		Contract:    adapter.OpenBaoV1Alpha1,
		CollectedAt: time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC),
		Limits:      adapter.DefaultProjectionLimits(),
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	plan, err := operations.PreparePause(cluster, adapter.OpenBaoV1Alpha1, true)
	if err != nil {
		fmt.Println(err)
		return
	}
	patch, err := plan.Patch()
	if err != nil {
		fmt.Println(err)
		return
	}
	var steps []json.RawMessage
	if err := json.Unmarshal(patch, &steps); err != nil {
		fmt.Println(err)
		return
	}
	for _, step := range steps {
		fmt.Println(string(step))
	}
	// Output:
	// {"op":"test","path":"/metadata/uid","value":"cluster-1"}
	// {"op":"test","path":"/metadata/resourceVersion","value":"42"}
	// {"op":"test","path":"/metadata/generation","value":4}
	// {"op":"add","path":"/spec/paused","value":true}
}
