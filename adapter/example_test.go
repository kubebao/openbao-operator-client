package adapter_test

import (
	"fmt"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
)

func ExampleProjectCluster() {
	raw := []byte(`{
		"apiVersion": "openbao.org/v1alpha1",
		"kind": "OpenBaoCluster",
		"metadata": {
			"namespace": "team",
			"name": "bao",
			"uid": "cluster-1",
			"generation": 4,
			"creationTimestamp": "2026-11-10T09:00:00Z"
		},
		"spec": {"replicas": 3, "backup": {}},
		"status": {"observedGeneration": 4, "readyReplicas": 3}
	}`)
	cluster, err := adapter.ProjectCluster(raw, adapter.ProjectionOptions{
		Origin:      "cluster-a",
		CollectedAt: time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC),
		Contract:    adapter.OpenBaoV1Alpha1,
		Limits:      adapter.DefaultProjectionLimits(),
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(cluster.Reference.Identity.Name, cluster.Acknowledgement())
	fmt.Println(*cluster.ReadyReplicas.Value, "of", *cluster.DesiredReplicas.Value, "ready")
	// Omitted spec.paused is implied false under the inspected contract.
	fmt.Println(cluster.Paused.State, *cluster.Paused.Value)
	// Output:
	// bao current
	// 3 of 3 ready
	// implied false
}

func ExampleProjectCluster_invalid() {
	_, err := adapter.ProjectCluster([]byte(`{"apiVersion":"v1","kind":"Pod"}`), adapter.ProjectionOptions{
		Origin:      "cluster-a",
		CollectedAt: time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC),
		Contract:    adapter.OpenBaoV1Alpha1,
		Limits:      adapter.DefaultProjectionLimits(),
	})
	fmt.Println(err)
	// Output:
	// resource does not match projection contract
}
