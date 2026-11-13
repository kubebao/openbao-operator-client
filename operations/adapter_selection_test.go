package operations_test

import (
	"errors"
	"testing"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
	"github.com/kubebao/openbao-operator-client/operations"
)

func TestControlPreparationRequiresSupportedBoundaryAdapter(t *testing.T) {
	t.Parallel()
	for _, contract := range []adapter.Contract{
		"", "unknown", adapter.OpenBaoV1Alpha1Unqualified, adapter.KubeBaoV1Alpha1Unqualified,
		adapter.KubeBaoV1Alpha1, adapter.KubernetesAppsV1, adapter.KubernetesCoreV1,
	} {
		for _, prepare := range []func(evidence.Cluster, adapter.Contract, bool) (operations.Plan, error){
			operations.PreparePause, operations.PrepareMaintenance,
		} {
			if _, err := prepare(
				controlCluster(t, ` ,"maintenance":{"enabled":false}`),
				contract,
				true,
			); !errors.Is(
				err,
				operations.ErrInvalidPlan,
			) {
				t.Fatalf("legacy target accepted boundary adapter %s: %v", contract, err)
			}
		}
	}
}
