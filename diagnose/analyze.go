package diagnose

import (
	"fmt"
	"slices"

	"github.com/kubebao/openbao-operator-client/evidence"
)

type Report struct {
	Policy   Policy     `json:"policy"`
	Findings []Finding  `json:"findings"`
	Coverage []Coverage `json:"coverage"`
}

type evaluation struct {
	state    CoverageState
	reason   CoverageReason
	reasons  []CoverageReason
	findings []Finding
}

// Analyze reads no clock or external state. Identical normalized evidence and
// policy produce the same report regardless of subject or rule selection order.
func Analyze(capture Capture, policy Policy) (Report, error) {
	result := Report{Findings: []Finding{}, Coverage: []Coverage{}}
	if err := policy.validate(); err != nil {
		return result, evidence.At("policy", err)
	}
	count := len(capture.Clusters)
	for _, input := range capture.Clusters {
		if input.StatefulSets != nil {
			count += len(input.StatefulSets.Observations)
		}
		if input.Pods != nil {
			count += len(input.Pods.Observations)
		}
		if input.Restores != nil {
			count += len(input.Restores.Observations)
		}
	}
	if count > policy.MaxSubjects {
		return result, invalidCapture("clusters", nil)
	}
	seen := map[evidence.Identity]bool{}
	for index, input := range capture.Clusters {
		if err := input.validate(policy); err != nil {
			return result, evidence.At(fmt.Sprintf("clusters[%d]", index), err)
		}
		if seen[input.Subject] {
			return result, evidence.At(fmt.Sprintf("clusters[%d].subject", index), ErrDuplicateSubject)
		}
		seen[input.Subject] = true
	}
	result.Policy = canonicalPolicy(policy)
	for _, input := range capture.Clusters {
		for _, rule := range SupportedRules() {
			coverage, findings := evaluate(input, rule, policy)
			if coverage.State == Insufficient && len(coverage.Reasons) == 0 {
				coverage.Reasons = []CoverageReason{coverage.Reason}
			}
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
	slices.Sort(policy.Rules)
	policy.ReferenceTime = policy.ReferenceTime.UTC()
	return policy
}

func evaluate(input ClusterInput, rule RuleID, policy Policy) (Coverage, []Finding) {
	coverage := Coverage{Rule: rule, Subject: input.Subject, State: Excluded, Reason: ReasonExcluded, Outcome: input.Collection.Outcome}
	if !slices.Contains(policy.Rules, rule) {
		return coverage, nil
	}
	if rule == RuleWorkload || rule == RulePod {
		return evaluateWorkloads(input, rule, policy, coverage)
	}
	if rule == RuleRestore {
		return evaluateRestores(input, policy, coverage)
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
	case RuleBackupFailed:
		value = backupFailures(*input.Observation)
	case RuleBackupAbsent:
		value = backupAbsent(*input.Observation)
	case RuleUpgrade:
		value = upgradeFailure(*input.Observation)
	case RuleRestoreTest:
		value = restoreTest(*input.Observation)
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
	if input.Collection.Outcome != evidence.CollectionComplete {
		value.insufficient(ReasonScopeIncomplete)
	}
	coverage.State, coverage.Reason, coverage.Reasons = value.state, value.reason, value.coverageReasons()
	return coverage, value.findings
}

func (e *evaluation) add(value *Finding) { e.findings = append(e.findings, *value) }

func (e *evaluation) insufficient(reason CoverageReason) {
	if e.state == Insufficient && e.reason != ReasonNone && !slices.Contains(e.reasons, e.reason) {
		e.reasons = append(e.reasons, e.reason)
	}
	if !slices.Contains(e.reasons, reason) {
		e.reasons = append(e.reasons, reason)
	}
	e.state, e.reason = Insufficient, reason
}

func (e evaluation) coverageReasons() []CoverageReason {
	if e.state != Insufficient {
		return nil
	}
	reasons := slices.Clone(e.reasons)
	if !slices.Contains(reasons, e.reason) {
		reasons = append(reasons, e.reason)
	}
	slices.Sort(reasons)
	return reasons
}

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
