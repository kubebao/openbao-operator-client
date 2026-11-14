// Command restart prints a guarded synthetic restart preview without network access.
package main

import (
	"fmt"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/operations"
)

func main() {
	raw := []byte(
		`{"apiVersion":"kubebao.org/v1alpha1",
"kind":"OpenBaoCluster",
"metadata":{"namespace":"team",
"name":"bao",
"uid":"cluster-1",
"resourceVersion":"10",
"generation":3},
"spec":{"replicas":1,
"paused":false,
"upgrade":{"strategy":"RollingUpdate"}},
"status":{"observedGeneration":3,
"acceptedUpgradeStrategy":"RollingUpdate"}}`,
	)
	cluster, err := adapter.ProjectCluster(
		raw,
		adapter.ProjectionOptions{
			Origin:      "example",
			CollectedAt: time.Date(2026, time.November, 14, 12, 0, 0, 0, time.UTC),
			Contract:    adapter.KubeBaoV1Alpha1,
			Limits:      adapter.DefaultProjectionLimits(),
		},
	)
	if err != nil {
		panic(err)
	}
	plan, err := operations.PrepareRestart(cluster, adapter.KubeBaoV1Alpha1, "2026-11-14T12:00:01Z")
	if err != nil {
		panic(err)
	}
	patch, err := plan.Patch()
	if err != nil {
		panic(err)
	}
	fmt.Println(string(patch))
}
