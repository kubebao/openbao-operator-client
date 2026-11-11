package adapter

import (
	"strings"

	"github.com/kubebao/openbao-operator-client/evidence"
)

// AssociateStatefulSet confirms a controller edge to the selected cluster.
// Annotation matches alone never prove ownership.
func AssociateStatefulSet(set evidence.StatefulSet, cluster evidence.Identity) (evidence.Association, error) {
	if set.Validate() != nil || cluster.Validate() != nil || cluster.Kind != "OpenBaoCluster" {
		return evidence.Association{}, &ProjectionError{Code: InvalidInput}
	}
	return controllerAssociation(set.Metadata, cluster, cluster), nil
}

func controllerAssociation(child evidence.WorkloadMetadata, parent, cluster evidence.Identity) evidence.Association {
	result := evidence.Association{Subject: child.Reference.Identity, Target: parent, State: evidence.AssociationUnconfirmed}
	if result.Subject.Origin != parent.Origin || result.Subject.Namespace != parent.Namespace {
		result.State = evidence.AssociationForeign
		return result
	}
	if child.Owners.State != evidence.ValueReported || child.ClusterOwnerUID.State == evidence.ValueInvalid {
		return result
	}
	controllers := []evidence.OwnerReference{}
	for _, ref := range child.Owners.Values {
		if ref.Controller {
			controllers = append(controllers, ref)
		}
	}
	if len(controllers) != 1 {
		return result
	}
	owner := controllers[0]
	annotation := textValue(child.ClusterOwnerUID)
	if owner.UID != parent.UID {
		result.State = evidence.AssociationForeign
		if parent == cluster && annotation == cluster.UID {
			result.State = evidence.AssociationConflict
		}
		return result
	}
	if owner.Group() != parent.Group || owner.Kind != parent.Kind || owner.Name != parent.Name ||
		annotation != "" && annotation != cluster.UID {
		result.State = evidence.AssociationConflict
		return result
	}
	result.State, result.Basis = evidence.AssociationConfirmed, evidence.BasisControllerOwner
	return result
}

// AssociatePod requires the supplied StatefulSet to be confirmed against a cluster.
// The parent association must describe this exact set UID and scope.
func AssociatePod(pod evidence.Pod, set evidence.StatefulSet, parent evidence.Association) (evidence.Association, error) {
	if pod.Validate() != nil || set.Validate() != nil || parent.Validate() != nil {
		return evidence.Association{}, &ProjectionError{Code: InvalidInput}
	}
	result := evidence.Association{
		Subject: pod.Metadata.Reference.Identity,
		Target:  set.Metadata.Reference.Identity,
		State:   evidence.AssociationUnconfirmed,
	}
	if parent.State != evidence.AssociationConfirmed || parent.Basis != evidence.BasisControllerOwner ||
		parent.Subject != result.Target || parent.Target.Kind != "OpenBaoCluster" {
		return result, nil
	}
	return controllerAssociation(pod.Metadata, result.Target, parent.Target), nil
}

// AssociateRestore uses only the pinned operator's UID-binding fields. A hold's
// operation ID and request age never prove target ownership. All supplied identity
// fields are checked before choosing a confirmation basis.
func AssociateRestore(target evidence.Cluster, request evidence.Restore) (evidence.Association, error) {
	if target.Validate() != nil || request.Validate() != nil {
		return evidence.Association{}, &ProjectionError{Code: InvalidInput}
	}
	owner, subject := target.Reference.Identity, request.Reference.Identity
	result := evidence.Association{Subject: subject, Target: owner, State: evidence.AssociationUnconfirmed}
	if subject.Origin != owner.Origin || subject.Namespace != owner.Namespace || subject.Group != owner.Group {
		result.State = evidence.AssociationForeign
		return result, nil
	}
	if target.AdapterContract != string(V1Alpha1Snapshot) || request.AdapterContract != string(V1Alpha1Snapshot) ||
		target.Reference.APIVersion != "openbao.org/v1alpha1" || request.Reference.APIVersion != "openbao.org/v1alpha1" {
		return result, nil
	}
	if request.Target.State == evidence.ObjectInvalid || request.Execution.State == evidence.ObjectInvalid {
		return result, nil
	}
	for _, value := range []evidence.Text{request.TargetName, request.Target.UID, request.Execution.TargetUID, target.RestoreOrigin} {
		if value.State == evidence.ValueInvalid {
			return result, nil
		}
	}
	name := textValue(request.TargetName)
	bound := textValue(request.Target.UID)
	execution := textValue(request.Execution.TargetUID)
	origin := textValue(target.RestoreOrigin)
	matched := bound == owner.UID || execution == owner.UID || origin == subject.UID
	conflict := name != "" && name != owner.Name || bound != "" && bound != owner.UID || execution != "" && execution != owner.UID
	if conflict {
		result.State = evidence.AssociationForeign
		if matched {
			result.State = evidence.AssociationConflict
		}
		return result, nil
	}
	switch {
	case execution == owner.UID:
		result.State, result.Basis = evidence.AssociationConfirmed, evidence.BasisRestoreExecution
	case bound == owner.UID:
		result.State, result.Basis = evidence.AssociationConfirmed, evidence.BasisRestoreTarget
	case origin == subject.UID:
		result.State, result.Basis = evidence.AssociationConfirmed, evidence.BasisRestoreOrigin
	case name == owner.Name:
		result.State = evidence.AssociationCandidate
	}
	return result, nil
}

func textValue(value evidence.Text) string {
	if value.Value == nil {
		return ""
	}
	return *value.Value
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
