package adapter

import (
	"strings"

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

// LatestRestore selects a confirmed request for display. Ties use UID ascending.
// Selection does not establish causality, submission acceptance, or operation success.
func LatestRestore(target evidence.Cluster, requests []evidence.Restore) (evidence.Restore, bool, error) {
	if target.Validate() != nil {
		return evidence.Restore{}, false, &ProjectionError{Code: InvalidInput}
	}
	seen := map[evidence.Identity]bool{}
	var latest evidence.Restore
	found := false
	for _, request := range requests {
		association, err := AssociateRestore(target, request)
		if err != nil {
			return evidence.Restore{}, false, err
		}
		id := request.Reference.Identity
		if seen[id] {
			return evidence.Restore{}, false, &ProjectionError{Code: InvalidInput}
		}
		seen[id] = true
		if association.State != evidence.AssociationConfirmed || request.CreatedAt.State != evidence.ValueReported ||
			request.CreatedAt.Value.After(*request.Reference.CollectedAt.Value) {
			continue
		}
		if !found || request.CreatedAt.Value.After(*latest.CreatedAt.Value) ||
			request.CreatedAt.Value.Equal(*latest.CreatedAt.Value) && strings.Compare(id.UID, latest.Reference.Identity.UID) < 0 {
			latest, found = request, true
		}
	}
	return latest, found, nil
}
