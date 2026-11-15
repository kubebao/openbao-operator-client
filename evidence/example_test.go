package evidence_test

import (
	"errors"
	"fmt"
	"time"

	"github.com/kubebao/openbao-operator-client/evidence"
)

func ExampleCompareGeneration() {
	fmt.Println(evidence.CompareGeneration(new(int64(4)), new(int64(4))))
	fmt.Println(evidence.CompareGeneration(new(int64(4)), new(int64(3))))
	fmt.Println(evidence.CompareGeneration(new(int64(4)), nil))
	// Output:
	// current
	// stale
	// unknown
}

func ExampleCollection_AbsenceConfirmed() {
	at := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	list := evidence.Collection{
		Scope: evidence.Scope{
			Origin:    "cluster-a",
			Group:     "openbao.org",
			Version:   "v1alpha1",
			Resource:  "openbaorestores",
			Namespace: "team",
			Action:    evidence.ReadList,
		},
		Interval: evidence.Interval{StartedAt: at, CompletedAt: at},
		Outcome:  evidence.CollectionComplete,
	}
	fmt.Println(list.AbsenceConfirmed())

	list.Outcome = evidence.CollectionPartial
	fmt.Println(list.AbsenceConfirmed())
	// Output:
	// true
	// false
}

func ExampleValidationError() {
	at := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	ref := evidence.Reference{
		Identity: evidence.Identity{
			Origin:    "cluster-a",
			Group:     "openbao.org",
			Kind:      "OpenBaoCluster",
			Namespace: "team",
			Name:      "bao",
			UID:       "uid\n1",
		},
		APIVersion:      "openbao.org/v1alpha1",
		ResourceVersion: evidence.Text{State: evidence.ValueUnknown},
		CollectedAt:     evidence.Timestamp{State: evidence.ValueReported, Value: &at},
	}
	err := ref.Validate()
	fmt.Println(err)
	fmt.Println(errors.Is(err, evidence.ErrInvalidEvidence))
	// Output:
	// invalid field: identity.uid
	// true
}
