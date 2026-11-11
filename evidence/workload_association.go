package evidence

// AssociateStatefulSet confirms a controller edge to the selected cluster.
// Annotation matches alone never prove ownership.
func AssociateStatefulSet(set StatefulSet, cluster Identity) (Association, error) {
	if set.Validate() != nil || cluster.Validate() != nil || cluster.Kind != "OpenBaoCluster" {
		return Association{}, ErrInvalidEvidence
	}
	return controllerAssociation(set.Metadata, cluster, cluster), nil
}

func controllerAssociation(child WorkloadMetadata, parent, cluster Identity) Association {
	result := Association{Subject: child.Reference.Identity, Target: parent, State: AssociationUnconfirmed}
	if result.Subject.Origin != parent.Origin || result.Subject.Namespace != parent.Namespace {
		result.State = AssociationForeign
		return result
	}
	if child.Owners.State != ValueReported || child.ClusterOwnerUID.State == ValueInvalid {
		return result
	}
	controllers := []OwnerReference{}
	for _, ref := range child.Owners.Values {
		if ref.Controller {
			controllers = append(controllers, ref)
		}
	}
	if len(controllers) != 1 {
		return result
	}
	owner := controllers[0]
	annotation := ""
	if child.ClusterOwnerUID.Value != nil {
		annotation = *child.ClusterOwnerUID.Value
	}
	if owner.UID != parent.UID {
		result.State = AssociationForeign
		if parent == cluster && annotation == cluster.UID {
			result.State = AssociationConflict
		}
		return result
	}
	if owner.Group() != parent.Group || owner.Kind != parent.Kind || owner.Name != parent.Name ||
		annotation != "" && annotation != cluster.UID {
		result.State = AssociationConflict
		return result
	}
	result.State, result.Basis = AssociationConfirmed, BasisControllerOwner
	return result
}

// AssociatePod requires the supplied StatefulSet to be confirmed against a cluster.
// The parent association must describe this exact set UID and scope.
func AssociatePod(pod Pod, set StatefulSet, parent Association) (Association, error) {
	if pod.Validate() != nil || set.Validate() != nil || parent.Validate() != nil {
		return Association{}, ErrInvalidEvidence
	}
	result := Association{
		Subject: pod.Metadata.Reference.Identity,
		Target:  set.Metadata.Reference.Identity,
		State:   AssociationUnconfirmed,
	}
	if parent.State != AssociationConfirmed || parent.Basis != BasisControllerOwner ||
		parent.Subject != result.Target || parent.Target.Kind != "OpenBaoCluster" {
		return result, nil
	}
	return controllerAssociation(pod.Metadata, result.Target, parent.Target), nil
}
