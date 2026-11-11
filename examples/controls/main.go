package main

import (
	"fmt"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/operations"
)

func main() {
	cluster, err := adapter.ProjectCluster([]byte(`{
  "apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster",
  "metadata":{"namespace":"team","name":"bao","uid":"cluster-1","resourceVersion":"10","generation":4},
  "spec":{"replicas":3,"paused":false},"status":{"observedGeneration":4}
}`), adapter.ProjectionOptions{
		Origin: "example-origin", Contract: adapter.V1Alpha1Snapshot,
		CollectedAt: time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC), Limits: adapter.DefaultProjectionLimits(),
	})
	if err != nil {
		panic(err)
	}
	plan, err := operations.PrepareMaintenance(cluster, true)
	if err != nil {
		panic(err)
	}
	preview, err := plan.Patch()
	if err != nil {
		panic(err)
	}
	fmt.Println(string(preview))
}
