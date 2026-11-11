package diagnose

import (
	"slices"

	"github.com/kubebao/openbao-operator-client/evidence"
)

type Report struct {
	SchemaVersion string     `json:"schemaVersion"`
	Policy        Policy     `json:"policy"`
	Findings      []Finding  `json:"findings"`
	Coverage      []Coverage `json:"coverage"`
}

type evaluation struct {
	state    CoverageState
	reason   CoverageReason
	findings []Finding
}

// Analyze reads no clock or external state. Identical normalized evidence and
// policy produce the same report regardless of subject or rule selection order.
func Analyze(capture Capture, policy Policy) (Report, error) {
	result := Report{Findings: []Finding{}, Coverage: []Coverage{}}
	if err := policy.validate(); err != nil {
		return result, err
	}
	if capture.SchemaVersion != evidence.SchemaVersion {
		return result, ErrUnsupportedVersion
	}
	if len(capture.Clusters) > policy.MaxSubjects {
		return result, ErrInvalidCapture
	}
	seen := map[evidence.Identity]bool{}
	for _, input := range capture.Clusters {
		if err := input.validate(policy); err != nil {
			return result, err
		}
		if seen[input.Subject] {
			return result, ErrDuplicateSubject
		}
		seen[input.Subject] = true
	}
	result.SchemaVersion, result.Policy = "1", canonicalPolicy(policy)
	for _, input := range capture.Clusters {
		for _, rule := range SupportedRules() {
			coverage, findings := evaluate(input, rule, policy)
			result.Coverage = append(result.Coverage, coverage)
			result.Findings = append(result.Findings, findings...)
		}
	}
	findings, err := CanonicalFindings(result.Findings)
	if err != nil {
		return Report{}, err
	}
	result.Findings = findings
	slices.SortFunc(result.Coverage, func(a, b Coverage) int {
		return slices.Compare((Finding{Rule: a.Rule, Subject: a.Subject}).parts(), (Finding{Rule: b.Rule, Subject: b.Subject}).parts())
	})
	return result, nil
}

func canonicalPolicy(policy Policy) Policy {
	policy.Rules = append([]RuleID{}, policy.Rules...)
	policy.ApprovedContracts = append([]string{}, policy.ApprovedContracts...)
	slices.Sort(policy.Rules)
	slices.Sort(policy.ApprovedContracts)
	policy.ReferenceTime = policy.ReferenceTime.UTC()
	return policy
}

func evaluate(input ClusterInput, rule RuleID, policy Policy) (Coverage, []Finding) {
	coverage := Coverage{Rule: rule, Subject: input.Subject, State: Excluded, Reason: ReasonExcluded, Outcome: input.Collection.Outcome}
	if !slices.Contains(policy.Rules, rule) {
		return coverage, nil
	}
	if input.Collection.Outcome == evidence.CollectionNotRequested {
		coverage.State, coverage.Reason = NotRequested, ReasonNotRequested
		return coverage, nil
	}
	coverage.State, coverage.Reason = Insufficient, ReasonMissingObservation
	if input.Collection.Outcome == evidence.CollectionDeleted || input.Collection.Outcome == evidence.CollectionReplaced {
		coverage.Reason = ReasonVerification
		return coverage, nil
	}
	if input.Observation == nil {
		return coverage, nil
	}
	read := *input.Observation.Reference.CollectedAt.Value
	if !input.Collection.Interval.Contains(read) || read.After(policy.ReferenceTime) {
		coverage.Reason = ReasonScopeIncomplete
		return coverage, nil
	}
	if policy.ReferenceTime.Sub(read) > policy.MaxEvidenceAge {
		coverage.Reason = ReasonStaleEvidence
		return coverage, nil
	}
	if input.Verification != nil && input.Verification.State != evidence.VerificationVerified {
		coverage.Reason = ReasonVerification
		return coverage, nil
	}
	var value evaluation
	switch rule {
	case RuleReadiness:
		value = readiness(*input.Observation, policy)
	case RuleBackupAge:
		value = backupAge(*input.Observation, policy)
	case RuleAvailability:
		value = availability(*input.Observation)
	case RuleSealed:
		value = sealed(*input.Observation)
	case RuleCondition:
		value = conditions(*input.Observation)
	case RuleController:
		value = controllerErrors(*input.Observation)
	case RuleGeneration:
		value = generation(*input.Observation)
	case RuleRestriction:
		value = restrictions(*input.Observation)
	}
	coverage.State, coverage.Reason = value.state, value.reason
	if input.Collection.Outcome != evidence.CollectionComplete {
		coverage.State, coverage.Reason = Insufficient, ReasonScopeIncomplete
	}
	return coverage, value.findings
}

func (e *evaluation) add(value *Finding) { e.findings = append(e.findings, *value) }

func (e *evaluation) insufficient(reason CoverageReason) { e.state, e.reason = Insufficient, reason }

func finding(cluster evidence.Cluster, rule RuleID, severity Severity, summary string, fields ...string) *Finding {
	value := &Finding{
		Rule:       rule,
		Subject:    cluster.Reference.Identity,
		Severity:   severity,
		Summary:    summary,
		References: []evidence.Reference{},
	}
	for _, field := range fields {
		ref := cluster.Reference
		ref.Field = field
		value.References = append(value.References, ref)
	}
	return value
}
