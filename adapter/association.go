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

// AssociateRestore compares normalized UID bindings. Cross-group resources remain
// foreign, including migrated resources.
func AssociateRestore(target evidence.Cluster, request evidence.Restore) (evidence.Association, error) {
	value, err := evidence.AssociateRestore(target, request)
	if err != nil {
		return evidence.Association{}, &ProjectionError{Code: InvalidInput}
	}
	return value, nil
}
