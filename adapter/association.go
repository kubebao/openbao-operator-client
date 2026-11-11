package adapter

import (
	"github.com/kubebao/openbao-operator-client/evidence"
)

// AssociateStatefulSet projects the pure UID association from normalized facts.
func AssociateStatefulSet(set evidence.StatefulSet, cluster evidence.Identity) (evidence.Association, error) {
	value, err := evidence.AssociateStatefulSet(set, cluster)
	if err != nil {
		return evidence.Association{}, &ProjectionError{Code: InvalidInput}
	}
	return value, nil
}

// AssociatePod requires the supplied StatefulSet's confirmed cluster association.
func AssociatePod(pod evidence.Pod, set evidence.StatefulSet, parent evidence.Association) (evidence.Association, error) {
	value, err := evidence.AssociatePod(pod, set, parent)
	if err != nil {
		return evidence.Association{}, &ProjectionError{Code: InvalidInput}
	}
	return value, nil
}

// AssociateRestore confirms only the inspected snapshot's public UID-binding contract.
func AssociateRestore(target evidence.Cluster, request evidence.Restore) (evidence.Association, error) {
	value, err := evidence.AssociateRestore(target, request, string(V1Alpha1Snapshot))
	if err != nil {
		return evidence.Association{}, &ProjectionError{Code: InvalidInput}
	}
	if value.State != evidence.AssociationForeign &&
		(target.Reference.APIVersion != "openbao.org/v1alpha1" || request.Reference.APIVersion != "openbao.org/v1alpha1") {
		value.State, value.Basis = evidence.AssociationUnconfirmed, ""
	}
	return value, nil
}
