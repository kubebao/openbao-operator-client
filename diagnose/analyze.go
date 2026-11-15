package diagnose

import (
	"slices"

	"github.com/kubebao/openbao-operator-client/evidence"
)

// Report contains the canonical policy, findings, and coverage of one analysis.
// Empty findings do not establish complete evidence or a healthy cluster.
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
	if capture.subjects() > policy.MaxSubjects {
		return result, invalidCapture("clusters", nil)
	}
	seen := map[evidence.Identity]bool{}
	for index, input := range capture.Clusters {
		path := indexPath("clusters", index)
		if err := input.validate(policy); err != nil {
			return result, evidence.At(path, err)
		}
		if seen[input.Subject] {
			return result, evidence.At(path+".subject", ErrDuplicateSubject)
		}
		seen[input.Subject] = true
	}
	result.Policy = canonicalPolicy(policy)
	for _, input := range capture.Clusters {
		for _, rule := range SupportedRules() {
			coverage, findings := evaluate(input, rule, policy)
			if coverage.State == CoverageInsufficient && len(coverage.Reasons) == 0 {
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
		return slices.Compare(
			Finding{Rule: a.Rule, Subject: a.Subject}.parts(),
			Finding{Rule: b.Rule, Subject: b.Subject}.parts(),
		)
	})
	return result, nil
}

// subjects counts the clusters and related observations that one analysis evaluates.
func (c Capture) subjects() int {
	count := len(c.Clusters)
	for _, input := range c.Clusters {
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
	return count
}

func canonicalPolicy(policy Policy) Policy {
	policy.Rules = append([]RuleID{}, policy.Rules...)
	slices.Sort(policy.Rules)
	policy.ReferenceTime = policy.ReferenceTime.UTC()
	return policy
}

func evaluate(input ClusterInput, rule RuleID, policy Policy) (Coverage, []Finding) {
	coverage := Coverage{
		Rule:    rule,
		Subject: input.Subject,
		State:   CoverageExcluded,
		Reason:  ReasonExcluded,
		Outcome: input.Collection.Outcome,
	}
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
		coverage.State = CoverageNotRequested
		coverage.Reason = ReasonNotRequested
		return coverage, nil
	}
	coverage.State = CoverageInsufficient
	coverage.Reason = ReasonMissingObservation
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
	value := evaluateCluster(*input.Observation, rule, policy)
	if input.Collection.Outcome != evidence.CollectionComplete {
		value.insufficient(ReasonScopeIncomplete)
	}
	return value.apply(coverage)
}

// evaluateCluster applies one rule that reads only the cluster observation.
func evaluateCluster(cluster evidence.Cluster, rule RuleID, policy Policy) evaluation {
	switch rule {
	case RuleReadiness:
		return readiness(cluster, policy)
	case RuleBackupAge:
		return backupAge(cluster, policy)
	case RuleBackupFailed:
		return backupFailures(cluster)
	case RuleBackupAbsent:
		return backupAbsent(cluster)
	case RuleUpgrade:
		return upgradeFailure(cluster)
	case RuleRestoreTest:
		return restoreTest(cluster)
	case RuleAvailability:
		return availability(cluster)
	case RuleSealed:
		return sealed(cluster)
	case RuleCondition:
		return conditions(cluster)
	case RuleController:
		return controllerErrors(cluster)
	case RuleGeneration:
		return generation(cluster)
	case RuleRestriction:
		return restrictions(cluster)
	default:
		return evaluation{}
	}
}

// apply records the evaluation's state and reasons on coverage and returns its findings.
func (e evaluation) apply(coverage Coverage) (Coverage, []Finding) {
	coverage.State = e.state
	coverage.Reason = e.reason
	coverage.Reasons = e.coverageReasons()
	return coverage, e.findings
}

func (e *evaluation) add(value *Finding) { e.findings = append(e.findings, *value) }

func (e *evaluation) insufficient(reason CoverageReason) {
	if e.state == CoverageInsufficient && e.reason != ReasonNone && !slices.Contains(e.reasons, e.reason) {
		e.reasons = append(e.reasons, e.reason)
	}
	if !slices.Contains(e.reasons, reason) {
		e.reasons = append(e.reasons, reason)
	}
	e.state = CoverageInsufficient
	e.reason = reason
}

func (e evaluation) coverageReasons() []CoverageReason {
	if e.state != CoverageInsufficient {
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
