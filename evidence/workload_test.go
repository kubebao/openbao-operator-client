package evidence_test

import (
	"testing"

	"github.com/kubebao/openbao-operator-client/evidence"
)

func TestOwnerSetsAreCanonicalAndIndependent(t *testing.T) {
	t.Parallel()
	values := []evidence.OwnerReference{
		{APIVersion: "apps/v1", Kind: "StatefulSet", Name: "b", UID: "b", Controller: true},
		{APIVersion: "apps/v1", Kind: "StatefulSet", Name: "a", UID: "a"},
	}
	owners, err := evidence.NewOwners(values)
	if err != nil {
		t.Fatal(err)
	}
	if owners.Values[0].UID != "a" || values[0].UID != "b" {
		t.Fatal("owner normalization mutated source or retained input order")
	}
	values[0].UID = "changed"
	if owners.Values[1].UID != "b" {
		t.Fatal("owner set shares mutable source storage")
	}
	_, err = evidence.NewOwners([]evidence.OwnerReference{owners.Values[0], owners.Values[0]})
	if err == nil {
		t.Fatal("duplicate owner UID was accepted")
	}
}

func TestAssociationScopeAndBasisValidation(t *testing.T) {
	t.Parallel()
	owner := evidence.Identity{
		Origin:    "origin",
		Group:     "openbao.org",
		Kind:      "OpenBaoCluster",
		Namespace: "team",
		Name:      "bao",
		UID:       "cluster-1",
	}
	subject := evidence.Identity{Origin: "origin", Group: "apps", Kind: "StatefulSet", Namespace: "team", Name: "bao", UID: "set-1"}
	value := evidence.Association{
		Subject: subject,
		Target:  owner,
		State:   evidence.AssociationConfirmed,
		Basis:   evidence.BasisControllerOwner,
	}
	if value.Validate() != nil {
		t.Fatal("valid controller relation rejected")
	}
	changed := value
	changed.Subject.Origin = "other"
	if changed.Validate() == nil {
		t.Fatal("confirmed cross-origin relation accepted")
	}
	changed = value
	changed.State = evidence.AssociationCandidate
	if changed.Validate() == nil {
		t.Fatal("candidate retained a confirmation basis")
	}
	changed = value
	changed.Basis = evidence.BasisRestoreTarget
	if changed.Validate() == nil {
		t.Fatal("StatefulSet retained restore confirmation basis")
	}
}
