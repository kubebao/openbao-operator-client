package evidence

// AssociateRestore compares normalized UID bindings for supported operator APIs.
// A hold's operation ID and request age never prove target ownership. All supplied
// identity fields are checked before choosing a confirmation basis.
func AssociateRestore(target Cluster, request Restore) (Association, error) {
	if target.Validate() != nil || request.Validate() != nil {
		return Association{}, ErrInvalidEvidence
	}
	owner, subject := target.Reference.Identity, request.Reference.Identity
	result := Association{Subject: subject, Target: owner, State: AssociationUnconfirmed}
	if subject.Origin != owner.Origin || subject.Namespace != owner.Namespace || subject.Group != owner.Group {
		result.State = AssociationForeign
		return result, nil
	}
	if (owner.Group != "openbao.org" && owner.Group != "kubebao.org") ||
		target.Reference.APIVersion != owner.Group+"/v1alpha1" ||
		target.Reference.APIVersion != request.Reference.APIVersion {
		return result, nil
	}
	if request.Target.State == ObjectInvalid || request.Execution.State == ObjectInvalid {
		return result, nil
	}
	for _, value := range []Text{request.TargetName, request.Target.UID, request.Execution.TargetUID, target.RestoreOrigin} {
		if value.State == ValueInvalid {
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
		result.State = AssociationForeign
		if matched {
			result.State = AssociationConflict
		}
		return result, nil
	}
	switch {
	case execution == owner.UID:
		result.State, result.Basis = AssociationConfirmed, BasisRestoreExecution
	case bound == owner.UID:
		result.State, result.Basis = AssociationConfirmed, BasisRestoreTarget
	case origin == subject.UID:
		result.State, result.Basis = AssociationConfirmed, BasisRestoreOrigin
	case name == owner.Name:
		result.State = AssociationCandidate
	}
	return result, nil
}

func textValue(value Text) string {
	if value.Value == nil {
		return ""
	}
	return *value.Value
}
