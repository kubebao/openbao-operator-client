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

// AssociateRestore confirms UID bindings only under the target's inspected source
// contract. Cross-group resources remain foreign, including migrated resources.
func AssociateRestore(target evidence.Cluster, request evidence.Restore) (evidence.Association, error) {
	contract := Contract(target.AdapterContract)
	info, describeErr := Describe(contract)
	bindingContract := ""
	if describeErr == nil && inspectedOperatorContract(contract) &&
		target.Reference.APIVersion == info.APIVersion && request.Reference.APIVersion == info.APIVersion &&
		target.Reference.Identity.Group == info.APIGroup && request.Reference.Identity.Group == info.APIGroup {
		bindingContract = string(contract)
	}
	value, err := evidence.AssociateRestore(target, request, bindingContract)
	if err != nil {
		return evidence.Association{}, &ProjectionError{Code: InvalidInput}
	}
	return value, nil
}
