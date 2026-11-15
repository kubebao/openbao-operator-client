package diagnose

import "github.com/kubebao/openbao-operator-client/evidence"

// CoverageState records whether a rule evaluated a subject and, if not, why.
type CoverageState string

const (
	// CoverageExcluded means the policy did not select the rule.
	CoverageExcluded CoverageState = "excluded"
	// CoverageNotRequested means the caller did not request the evidence the rule reads.
	CoverageNotRequested CoverageState = "not_requested"
	// CoverageNotApplicable means the rule does not apply to the subject.
	CoverageNotApplicable CoverageState = "not_applicable"
	// CoverageInsufficient means the evidence cannot support a complete evaluation.
	// Findings can still accompany insufficient coverage.
	CoverageInsufficient CoverageState = "insufficient"
	// CoverageEvaluated means the rule evaluated complete evidence.
	CoverageEvaluated CoverageState = "evaluated"
)

// Applicability records whether a rule applies to a subject.
type Applicability string

const (
	// ApplicabilityApplies means the rule applies to the subject.
	ApplicabilityApplies Applicability = "applicable"
	// ApplicabilityDoesNotApply means the rule does not apply to the subject.
	ApplicabilityDoesNotApply Applicability = "not_applicable"
	// ApplicabilityUnknown means the evidence does not decide applicability.
	ApplicabilityUnknown Applicability = "unknown"
)

// CoverageInput separates the facts that ResolveCoverage combines.
type CoverageInput struct {
	// Selected reports whether the policy selects the rule.
	Selected bool
	// Requested reports whether the caller requested the rule's evidence.
	Requested bool
	// Applicability records whether the rule applies to the subject.
	Applicability Applicability
	// Collection records the read that supplied the evidence.
	Collection evidence.Collection
	// EvidenceValid reports whether the collected evidence passed validation.
	EvidenceValid bool
}

// CoverageResult is the coverage that ResolveCoverage derives from a CoverageInput.
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
	case input.Collection.Validate() != nil,
		input.Collection.Outcome != evidence.CollectionComplete,
		!input.EvidenceValid:
		return CoverageResult{State: CoverageInsufficient}
	case input.Applicability == ApplicabilityDoesNotApply:
		return CoverageResult{State: CoverageNotApplicable}
	case input.Applicability != ApplicabilityApplies:
		return CoverageResult{State: CoverageInsufficient}
	default:
		return CoverageResult{State: CoverageEvaluated, AbsenceConfirmed: input.Collection.AbsenceConfirmed()}
	}
}

// CoverageReason explains a coverage state other than a complete evaluation.
type CoverageReason string

const (
	// ReasonNone means the rule evaluated without a limiting cause.
	ReasonNone CoverageReason = "none"
	// ReasonExcluded means the policy did not select the rule.
	ReasonExcluded CoverageReason = "excluded"
	// ReasonNotRequested means the caller did not request the rule's evidence.
	ReasonNotRequested CoverageReason = "not_requested"
	// ReasonScopeIncomplete means a read was incomplete or an observation lies outside its read interval.
	ReasonScopeIncomplete CoverageReason = "scope_incomplete"
	// ReasonMissingObservation means a required observation was not supplied.
	ReasonMissingObservation CoverageReason = "missing_observation"
	// ReasonStaleEvidence means an observation is older than the policy's evidence age limit.
	ReasonStaleEvidence CoverageReason = "stale_evidence"
	// ReasonMissingField means a required field is unknown, invalid, or has an unrecognized value.
	ReasonMissingField CoverageReason = "missing_field"
	// ReasonGeneration means the producer has not acknowledged the current generation.
	ReasonGeneration CoverageReason = "generation_unconfirmed"
	// ReasonTime means a reported timestamp is invalid or later than its read.
	ReasonTime CoverageReason = "inconsistent_time"
	// ReasonNotConfigured means the rule's feature is not configured.
	ReasonNotConfigured CoverageReason = "not_configured"
	// ReasonHigherPriority means a higher-priority backup rule covers the reported failures.
	ReasonHigherPriority CoverageReason = "higher_priority_backup_signal"
	// ReasonVerification means a later read did not verify the selected cluster.
	ReasonVerification CoverageReason = "target_unverified"
	// ReasonAssociation means a related resource's UID association is unconfirmed.
	ReasonAssociation CoverageReason = "association_unconfirmed"
	// ReasonScopeRestricted means a selector narrowed a read below the scope the rule needs.
	ReasonScopeRestricted CoverageReason = "scope_restricted"
)

// Coverage records how one rule covered one subject.
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
