package evidence

// AssociationState classifies the relationship between two resources.
type AssociationState string

const (
	// AssociationConfirmed means a documented UID binding links the subject to the target.
	AssociationConfirmed AssociationState = "confirmed"
	// AssociationCandidate means names agree but no UID binding confirms the association.
	AssociationCandidate AssociationState = "candidate"
	// AssociationUnconfirmed means the evidence neither confirms nor excludes the association.
	AssociationUnconfirmed AssociationState = "unconfirmed"
	// AssociationForeign means the evidence binds the subject to another resource.
	AssociationForeign AssociationState = "foreign"
	// AssociationConflict means bindings both match and contradict the target.
	AssociationConflict AssociationState = "conflict"
)

// AssociationBasis names the documented field that confirms an association.
type AssociationBasis string

const (
	// BasisControllerOwner confirms an association through a controller owner reference.
	BasisControllerOwner AssociationBasis = "metadata.ownerReferences"
	// BasisRestoreExecution confirms a restore through its execution target UID.
	BasisRestoreExecution AssociationBasis = "status.execution.targetUID"
	// BasisRestoreTarget confirms a restore through its bound target UID.
	BasisRestoreTarget AssociationBasis = "status.target.uid"
	// BasisRestoreOrigin confirms a restore through the cluster's restore-origin annotation.
	BasisRestoreOrigin AssociationBasis = "metadata.annotations[openbao.org/restore-origin]"
)

// Association records the comparison of a subject with a target. Target is the
// identity being compared, not a declaration of ownership. Only confirmed
// associations carry a basis. Every association retains full scope.
type Association struct {
	Subject Identity         `json:"subject"`
	Target  Identity         `json:"target"`
	State   AssociationState `json:"state"`
	Basis   AssociationBasis `json:"basis,omitempty"`
}

// Validate checks both identities and that only a confirmed association within
// one origin and namespace carries a basis that applies to its kinds.
func (v Association) Validate() error {
	if err := checkFields(
		fieldCheck{"subject", v.Subject.Validate()},
		fieldCheck{"target", v.Target.Validate()},
	); err != nil {
		return err
	}
	if v.State == AssociationConfirmed {
		return v.validateConfirmed()
	}
	if v.Basis != "" {
		return At("basis", ErrInvalidEvidence)
	}
	switch v.State {
	case AssociationCandidate, AssociationUnconfirmed, AssociationForeign, AssociationConflict:
		return nil
	default:
		return At("state", ErrInvalidEvidence)
	}
}

func (v Association) validateConfirmed() error {
	if v.Subject.Origin != v.Target.Origin || v.Subject.Namespace != v.Target.Namespace {
		return At("target", ErrInvalidEvidence)
	}
	switch v.Basis {
	case BasisControllerOwner:
		return nil
	case BasisRestoreExecution, BasisRestoreTarget, BasisRestoreOrigin:
		if v.Subject.Kind == "OpenBaoRestore" && v.Target.Kind == "OpenBaoCluster" && v.Subject.Group == v.Target.Group {
			return nil
		}
	}
	return At("basis", ErrInvalidEvidence)
}
