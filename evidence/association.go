package evidence

type AssociationState string

const (
	AssociationConfirmed   AssociationState = "confirmed"
	AssociationCandidate   AssociationState = "candidate"
	AssociationUnconfirmed AssociationState = "unconfirmed"
	AssociationForeign     AssociationState = "foreign"
	AssociationConflict    AssociationState = "conflict"
)

type AssociationBasis string

const (
	BasisControllerOwner  AssociationBasis = "metadata.ownerReferences"
	BasisRestoreExecution AssociationBasis = "status.execution.targetUID"
	BasisRestoreTarget    AssociationBasis = "status.target.uid"
	BasisRestoreOrigin    AssociationBasis = "metadata.annotations[openbao.org/restore-origin]"
)

// Target is the identity being compared, not a declaration of ownership.
// Only confirmed associations carry a basis. Every association retains full scope.
type Association struct {
	Subject Identity         `json:"subject"`
	Target  Identity         `json:"target"`
	State   AssociationState `json:"state"`
	Basis   AssociationBasis `json:"basis,omitempty"`
}

func (v Association) Validate() error {
	if v.Subject.Validate() != nil || v.Target.Validate() != nil {
		return ErrInvalidEvidence
	}
	if v.State == AssociationConfirmed {
		if v.Subject.Origin != v.Target.Origin || v.Subject.Namespace != v.Target.Namespace {
			return ErrInvalidEvidence
		}
		switch v.Basis {
		case BasisControllerOwner:
			return nil
		case BasisRestoreExecution, BasisRestoreTarget, BasisRestoreOrigin:
			if v.Subject.Kind == "OpenBaoRestore" && v.Target.Kind == "OpenBaoCluster" && v.Subject.Group == v.Target.Group {
				return nil
			}
		}
		return ErrInvalidEvidence
	}
	if v.Basis != "" {
		return ErrInvalidEvidence
	}
	switch v.State {
	case AssociationCandidate, AssociationUnconfirmed, AssociationForeign, AssociationConflict:
		return nil
	default:
		return ErrInvalidEvidence
	}
}
