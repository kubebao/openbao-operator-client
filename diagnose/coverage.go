package diagnose

import "github.com/kubebao/openbao-operator-client/evidence"

type CoverageState string

const (
	Excluded      CoverageState = "excluded"
	NotRequested  CoverageState = "not_requested"
	NotApplicable CoverageState = "not_applicable"
	Insufficient  CoverageState = "insufficient"
	Evaluated     CoverageState = "evaluated"
)

type Applicability string

const (
	Applies              Applicability = "applicable"
	DoesNotApply         Applicability = "not_applicable"
	ApplicabilityUnknown Applicability = "unknown"
)

type CoverageInput struct {
	Selected      bool
	Requested     bool
	Applicability Applicability
	Collection    evidence.Collection
	EvidenceValid bool
}

type CoverageResult struct {
	State            CoverageState `json:"state"`
	AbsenceConfirmed bool          `json:"absenceConfirmed"`
}

// ResolveCoverage separates rule selection, requested scope, applicability, and evidence.
// Only a valid, complete empty LIST can establish absence in its declared scope.
func ResolveCoverage(input CoverageInput) CoverageResult {
	switch {
	case !input.Selected:
		return CoverageResult{State: Excluded}
	case !input.Requested:
		return CoverageResult{State: NotRequested}
	case input.Collection.Validate() != nil || input.Collection.Outcome != evidence.CollectionComplete || !input.EvidenceValid:
		return CoverageResult{State: Insufficient}
	case input.Applicability == DoesNotApply:
		return CoverageResult{State: NotApplicable}
	case input.Applicability != Applies:
		return CoverageResult{State: Insufficient}
	default:
		return CoverageResult{State: Evaluated, AbsenceConfirmed: input.Collection.AbsenceConfirmed()}
	}
}

type CoverageReason string

const (
	ReasonNone               CoverageReason = "none"
	ReasonExcluded           CoverageReason = "excluded"
	ReasonNotRequested       CoverageReason = "not_requested"
	ReasonScopeIncomplete    CoverageReason = "scope_incomplete"
	ReasonMissingObservation CoverageReason = "missing_observation"
	ReasonStaleEvidence      CoverageReason = "stale_evidence"
	ReasonMissingField       CoverageReason = "missing_field"
	ReasonGeneration         CoverageReason = "generation_unconfirmed"
	ReasonTime               CoverageReason = "inconsistent_time"
	ReasonNotConfigured      CoverageReason = "not_configured"
	ReasonHigherPriority     CoverageReason = "higher_priority_backup_signal"
	ReasonVerification       CoverageReason = "target_unverified"
)

type Coverage struct {
	Rule    RuleID                     `json:"rule"`
	Subject evidence.Identity          `json:"subject"`
	State   CoverageState              `json:"state"`
	Reason  CoverageReason             `json:"reason"`
	Outcome evidence.CollectionOutcome `json:"collectionOutcome"`
}
