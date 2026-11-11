package diagnose_test

import (
	"testing"

	"github.com/kubebao/openbao-operator-client/diagnose"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func TestLatestRestoreRequiresBindingContractAndRejectsDuplicateOrFutureRecords(t *testing.T) {
	t.Parallel()
	i := restoreInput(t)
	request := i.Restores.Observations[0]
	for _, contract := range []string{"", "other", i.AdapterContract} {
		_, found, err := diagnose.LatestRestore(*i.Observation, []evidence.Restore{request}, contract, "openbao.org/v1alpha1")
		if err != nil || found != (contract == i.AdapterContract) {
			t.Fatalf("display selection ignored the selected binding contract: %v", err)
		}
	}
	if _, _, err := diagnose.LatestRestore(
		*i.Observation,
		[]evidence.Restore{request, request},
		i.AdapterContract,
		"openbao.org/v1alpha1",
	); err == nil {
		t.Fatal("duplicate restore identities were accepted")
	}
	otherTarget, otherRequest := *i.Observation, request
	otherTarget.Reference.APIVersion, otherRequest.Reference.APIVersion = "openbao.org/v1beta1", "openbao.org/v1beta1"
	if _, found, err := diagnose.LatestRestore(
		otherTarget,
		[]evidence.Restore{otherRequest},
		i.AdapterContract,
		"openbao.org/v1alpha1",
	); err != nil ||
		found {
		t.Fatal("a matching contract label approved another API version")
	}
	future := request.Reference.CollectedAt.Value.AddDate(0, 1, 0)
	request.CreatedAt = evidence.Timestamp{State: evidence.ValueReported, Value: &future}
	if _, found, err := diagnose.LatestRestore(
		*i.Observation,
		[]evidence.Restore{request},
		i.AdapterContract,
		"openbao.org/v1alpha1",
	); err != nil ||
		found {
		t.Fatal("a future creation time selected a display request")
	}
}
