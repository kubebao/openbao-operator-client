package adapter_test

import (
	"encoding/json"
	"testing"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func TestUnqualifiedAdaptersDoNotProjectRestoreUIDBindings(t *testing.T) {
	t.Parallel()
	for _, contract := range []adapter.Contract{adapter.OpenBaoV1Alpha1Unqualified, adapter.KubeBaoV1Alpha1Unqualified} {
		t.Run(string(contract), func(t *testing.T) {
			options := projectionOptions()
			options.Contract = contract
			clusterData, restoreData := operationalFixture(t), restoreFixture(t)
			if contract == adapter.KubeBaoV1Alpha1Unqualified {
				clusterData, restoreData = nativeDocument(clusterData), nativeDocument(restoreData)
			}
			cluster, err := adapter.ProjectCluster(clusterData, options)
			if err != nil {
				t.Fatal(err)
			}
			request, err := adapter.ProjectRestore(restoreData, options)
			if err != nil {
				t.Fatal(err)
			}
			if cluster.RestoreOrigin.State != evidence.ValueUnknown || request.Target.UID.State != evidence.ValueUnknown ||
				request.Execution.TargetUID.State != evidence.ValueUnknown {
				t.Fatal("unqualified projection retained interpreted UID-binding facts")
			}
			if request.TargetName.State != evidence.ValueReported || request.Phase.State != evidence.ValueReported {
				t.Fatal("unqualified projection discarded independent reported fields")
			}
			raw, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			var replay evidence.Restore
			if err := json.Unmarshal(raw, &replay); err != nil {
				t.Fatal(err)
			}
			association, err := evidence.AssociateRestore(cluster, replay)
			if err != nil || association.State != evidence.AssociationCandidate {
				t.Fatalf("unqualified UID binding confirmed after replay: %+v, %v", association, err)
			}
		})
	}
}
