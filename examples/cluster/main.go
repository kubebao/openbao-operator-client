package main

import (
	"fmt"
	"log"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
)

func main() {
	raw := []byte(`{
  "apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster",
  "metadata":{"namespace":"team","name":"bao","uid":"cluster-1","generation":4},
  "spec":{"replicas":3},"status":{"observedGeneration":4}
}`)
	cluster, err := adapter.ProjectCluster(raw, adapter.ProjectionOptions{
		Origin: "example-origin", Contract: adapter.V1Alpha1Snapshot,
		CollectedAt: time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC), Limits: adapter.DefaultProjectionLimits(),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("generation: %s\nready replicas: %s", cluster.Acknowledgement(), cluster.ReadyReplicas.State)
	if value := cluster.ReadyReplicas.Value; value != nil {
		fmt.Printf(" (%d)", *value)
	}
	fmt.Println()
}
