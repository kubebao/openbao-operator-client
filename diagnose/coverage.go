package diagnose

import "github.com/kubebao/openbao-operator-client/evidence"

type CoverageState string

const (
	CoverageExcluded      CoverageState = "excluded"
	CoverageNotRequested  CoverageState = "not_requested"
	CoverageNotApplicable CoverageState = "not_applicable"
	CoverageInsufficient  CoverageState = "insufficient"
	CoverageEvaluated     CoverageState = "evaluated"
)

type Applicability string

const (
	ApplicabilityApplies      Applicability = "applicable"
	ApplicabilityDoesNotApply Applicability = "not_applicable"
	ApplicabilityUnknown      Applicability = "unknown"
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
		return CoverageResult{State: CoverageExcluded}
	case !input.Requested:
		return CoverageResult{State: CoverageNotRequested}
	case input.Collection.Validate() != nil || input.Collection.Outcome != evidence.CollectionComplete || !input.EvidenceValid:
		return CoverageResult{State: CoverageInsufficient}
	case input.Applicability == ApplicabilityDoesNotApply:
		return CoverageResult{State: CoverageNotApplicable}
	case input.Applicability != ApplicabilityApplies:
		return CoverageResult{State: CoverageInsufficient}
	default:
		return CoverageResult{State: CoverageEvaluated, AbsenceConfirmed: input.Collection.AbsenceConfirmed()}
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
	ReasonAssociation        CoverageReason = "association_unconfirmed"
	ReasonScopeRestricted    CoverageReason = "scope_restricted"
)

type Coverage struct {
	Rule    RuleID            `json:"rule"`
	Subject evidence.Identity `json:"subject"`
	State   CoverageState     `json:"state"`
	Reason  CoverageReason    `json:"reason"`
	// Reasons retains every observed insufficiency cause in canonical order.
	// Reason preserves the legacy primary cause for existing consumer mappings.
	Reasons []CoverageReason           `json:"reasons,omitempty"`
	Outcome evidence.CollectionOutcome `json:"collectionOutcome"`
}
