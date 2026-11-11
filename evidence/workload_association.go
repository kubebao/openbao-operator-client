package evidence

// PodHasNonStatefulSetController reports one valid controller reference that
// identifies another kind or API group. Callers must first reject UID conflicts
// with known StatefulSets before using this fact to exclude membership.
// Missing or multiple controller references do not prove exclusion. This does
// not establish whether a Job or another controller belongs to a cluster.
func PodHasNonStatefulSetController(pod Pod) (bool, error) {
	if err := pod.Validate(); err != nil {
		return false, err
	}
	if pod.Metadata.Owners.State != ValueReported {
		return false, nil
	}
	var controller *OwnerReference
	for _, owner := range pod.Metadata.Owners.Values {
		if !owner.Controller {
			continue
		}
		if controller != nil {
			return false, nil
		}
		copyOwner := owner
		controller = &copyOwner
	}
	return controller != nil && (controller.Kind != "StatefulSet" || controller.Group() != "apps"), nil
}

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

// AssociatePod confirms a Pod chain or excludes a proven foreign chain.
// The parent association must match this exact StatefulSet metadata and scope.
func AssociatePod(pod Pod, set StatefulSet, parent Association) (Association, error) {
	if pod.Validate() != nil || set.Validate() != nil || parent.Validate() != nil {
		return Association{}, ErrInvalidEvidence
	}
	result := Association{
		Subject: pod.Metadata.Reference.Identity,
		Target:  set.Metadata.Reference.Identity,
		State:   AssociationUnconfirmed,
	}
	if parent.Subject != result.Target || parent.Target.Kind != "OpenBaoCluster" {
		return result, nil
	}
	expected := controllerAssociation(set.Metadata, parent.Target, parent.Target)
	if parent != expected {
		return result, nil
	}
	if parent.State == AssociationForeign {
		return foreignPodAssociation(pod, set, parent), nil
	}
	if parent.State != AssociationConfirmed || parent.Basis != BasisControllerOwner {
		return result, nil
	}
	return controllerAssociation(pod.Metadata, result.Target, parent.Target), nil
}

// A foreign parent excludes a Pod only when both controller edges and reserved
// annotations agree. An unrelated parent assertion cannot prove exclusion.
func foreignPodAssociation(pod Pod, set StatefulSet, parent Association) Association {
	result := Association{Subject: pod.Metadata.Reference.Identity, Target: set.Metadata.Reference.Identity, State: AssociationUnconfirmed}
	for _, owner := range set.Metadata.Owners.Values {
		if !owner.Controller || owner.Kind != "OpenBaoCluster" || owner.Group() != parent.Target.Group || owner.UID == parent.Target.UID {
			continue
		}
		foreign := parent.Target
		foreign.Name, foreign.UID = owner.Name, owner.UID
		edge := controllerAssociation(pod.Metadata, set.Metadata.Reference.Identity, foreign)
		if edge.State == AssociationConfirmed {
			edge.State, edge.Basis = AssociationForeign, ""
		}
		return edge
	}
	return result
}
