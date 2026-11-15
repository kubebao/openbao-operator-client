package diagnose

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/kubebao/openbao-operator-client/evidence"
)

var (
	// ErrUnknownRule reports a policy rule that is not a defined RuleID.
	ErrUnknownRule = errors.New("unknown rule")
	// ErrUnavailableRule reports a policy rule that is defined but not supported.
	ErrUnavailableRule = errors.New("rule is not implemented")
	// ErrDuplicateSubject reports two cluster inputs with the same subject.
	ErrDuplicateSubject = errors.New("duplicate subject")
	// ErrInvalidCapture reports a capture that fails validation.
	ErrInvalidCapture = errors.New("invalid normalized capture")
	// ErrInvalidPolicy reports a policy that fails validation.
	ErrInvalidPolicy = errors.New("invalid evaluation policy")
)

// SupportedRules describes executable rules, separately from the canonical ID inventory.
func SupportedRules() []RuleID {
	return []RuleID{
		RuleBackupFailed,
		RuleBackupAbsent,
		RuleBackupAge,
		RuleAvailability,
		RuleSealed,
		RuleReadiness,
		RuleCondition,
		RuleController,
		RuleGeneration,
		RulePod,
		RuleRestore,
		RuleRestoreTest,
		RuleRestriction,
		RuleUpgrade,
		RuleWorkload,
	}
}

// ClusterInput binds a selected identity to its read outcome. A nil observation
// preserves coverage for a known subject after a failed or unrequested read.
type ClusterInput struct {
	Subject      evidence.Identity                    `json:"subject"`
	Observation  *evidence.Cluster                    `json:"observation,omitempty"`
	Collection   evidence.Collection                  `json:"collection"`
	Verification *evidence.TargetVerification         `json:"verification,omitempty"`
	StatefulSets *ResourceInput[evidence.StatefulSet] `json:"statefulSets,omitempty"`
	Pods         *ResourceInput[evidence.Pod]         `json:"pods,omitempty"`
	Restores     *ResourceInput[evidence.Restore]     `json:"restores,omitempty"`
}

// ResourceInput retains the declared read and every admitted candidate before
// association filtering. Nil inputs on ClusterInput mean no scope was requested.
type ResourceInput[T any] struct {
	Collection   evidence.Collection `json:"collection"`
	Observations []T                 `json:"observations"`
}

// Capture contains normalized evidence, not raw Kubernetes resources or product state.
// Consumers own decoding and input byte limits. Analyze validates typed shapes.
type Capture struct {
	Clusters []ClusterInput `json:"clusters"`
}

// Policy selects rules and supplies the reference time and bounds of an analysis.
// Analyze reads no clock; ReferenceTime is the only time it compares against.
type Policy struct {
	// Rules selects the supported rules to evaluate. Each rule appears at most once.
	Rules []RuleID `json:"rules"`
	// ReportHistoricalRestores reports recorded restore outcomes that require
	// investigation when no matching restore hold binds them.
	ReportHistoricalRestores bool `json:"reportHistoricalRestores"`
	// ReferenceTime is the instant against which ages and freshness are measured.
	ReferenceTime time.Time `json:"referenceTime"`
	// MaxBackupAge is the largest accepted age of the last successful backup.
	MaxBackupAge time.Duration `json:"maxBackupAge"`
	// StartupGrace delays readiness findings after cluster creation.
	StartupGrace time.Duration `json:"startupGrace"`
	// MaxEvidenceAge is the largest accepted age of an observation's read.
	MaxEvidenceAge time.Duration `json:"maxEvidenceAge"`
	// MaxSubjects bounds the clusters and related observations in one capture.
	MaxSubjects int `json:"maxSubjects"`
}

// DefaultPolicy selects every supported rule at the given reference time. It allows
// backups up to 24 hours old, a one-minute startup grace, evidence up to five
// minutes old, and 5000 subjects, and it reports historical restores.
func DefaultPolicy(reference time.Time) Policy {
	return Policy{
		Rules:                    SupportedRules(),
		ReportHistoricalRestores: true,
		ReferenceTime:            reference.UTC(),
		MaxBackupAge:             24 * time.Hour,
		StartupGrace:             time.Minute,
		MaxEvidenceAge:           5 * time.Minute,
		MaxSubjects:              5000,
	}
}

func (p Policy) validate() error {
	if p.ReferenceTime.IsZero() || p.ReferenceTime.Year() < 1 || p.ReferenceTime.Year() > 9999 {
		return evidence.At("referenceTime", ErrInvalidPolicy)
	}
	for _, field := range []struct {
		name  string
		valid bool
	}{
		{"maxBackupAge", p.MaxBackupAge > 0},
		{"startupGrace", p.StartupGrace >= 0},
		{"maxEvidenceAge", p.MaxEvidenceAge > 0},
		{"maxSubjects", p.MaxSubjects > 0},
	} {
		if !field.valid {
			return evidence.At(field.name, ErrInvalidPolicy)
		}
	}
	supported := SupportedRules()
	seen := map[RuleID]bool{}
	for i, rule := range p.Rules {
		path := indexPath("rules", i)
		if !rule.Valid() {
			return evidence.At(path, ErrUnknownRule)
		}
		if !slices.Contains(supported, rule) {
			return evidence.At(path, ErrUnavailableRule)
		}
		if seen[rule] {
			return evidence.At(path, ErrInvalidPolicy)
		}
		seen[rule] = true
	}
	return nil
}

func invalidCapture(path string, cause error) error {
	return evidence.At(path, errors.Join(ErrInvalidCapture, cause))
}

func (c ClusterInput) validate(p Policy) error {
	if err := c.Subject.Validate(); err != nil {
		return invalidCapture("subject", err)
	}
	if c.Subject.Kind != "OpenBaoCluster" {
		return invalidCapture("subject.kind", nil)
	}
	if err := c.Collection.Validate(); err != nil {
		return invalidCapture("collection", err)
	}
	if err := c.validateScope(); err != nil {
		return err
	}
	if !c.Collection.Interval.CompletedAt.IsZero() && c.Collection.Interval.CompletedAt.After(p.ReferenceTime) {
		return invalidCapture("collection.interval.completedAt", nil)
	}
	if err := c.validateObservation(); err != nil {
		return err
	}
	if err := c.validateVerification(p); err != nil {
		return err
	}
	return c.validateRelatedInputs(p)
}

func (c ClusterInput) validateScope() error {
	scope := c.Collection.Scope
	if c.Subject.Group != "openbao.org" && c.Subject.Group != "kubebao.org" {
		return invalidCapture("subject.group", nil)
	}
	if scope.Origin != c.Subject.Origin {
		return invalidCapture("collection.scope.origin", nil)
	}
	if scope.Group != c.Subject.Group {
		return invalidCapture("collection.scope.group", nil)
	}
	if scope.Version != "v1alpha1" {
		return invalidCapture("collection.scope.version", nil)
	}
	if scope.Resource != "openbaoclusters" {
		return invalidCapture("collection.scope.resource", nil)
	}
	if scope.Action != evidence.ReadGet && scope.Action != evidence.ReadList {
		return invalidCapture("collection.scope.action", nil)
	}
	if scope.Action == evidence.ReadGet && scope.Namespace == "" {
		return invalidCapture("collection.scope.namespace", nil)
	}
	if scope.Namespace != "" && scope.Namespace != c.Subject.Namespace {
		return invalidCapture("collection.scope.namespace", nil)
	}
	if scope.Name != "" && scope.Name != c.Subject.Name {
		return invalidCapture("collection.scope.name", nil)
	}
	return nil
}

func (c ClusterInput) validateObservation() error {
	if c.Observation == nil {
		return nil
	}
	o := c.Observation
	if err := o.Validate(); err != nil {
		return invalidCapture("observation", err)
	}
	if o.Reference.Identity != c.Subject {
		return invalidCapture("observation.reference.identity", nil)
	}
	if strings.TrimPrefix(o.Reference.APIVersion, c.Subject.Group+"/") != c.Collection.Scope.Version {
		return invalidCapture("observation.reference.apiVersion", nil)
	}
	if c.Collection.Outcome == evidence.CollectionComplete {
		if c.Collection.Items == 0 {
			return invalidCapture("collection.items", nil)
		}
		if !c.Collection.Interval.Contains(*o.Reference.CollectedAt.Value) {
			return invalidCapture("observation.reference.collectedAt", nil)
		}
	}
	return nil
}

func (c ClusterInput) validateVerification(p Policy) error {
	if c.Verification == nil {
		return nil
	}
	v := c.Verification
	if err := v.Validate(); err != nil {
		return invalidCapture("verification", err)
	}
	if v.Before.Identity != c.Subject {
		return invalidCapture("verification.before.identity", nil)
	}
	if v.Read.Interval.CompletedAt.After(p.ReferenceTime) {
		return invalidCapture("verification.read.interval.completedAt", nil)
	}
	if c.Observation != nil {
		if !slices.Equal(referenceParts(v.Before), referenceParts(c.Observation.Reference)) {
			return invalidCapture("verification.before", nil)
		}
		if !equalInteger(v.BeforeGeneration, c.Observation.Generation) {
			return invalidCapture("verification.beforeGeneration", nil)
		}
	}
	return nil
}

func equalInteger(a, b evidence.Integer) bool {
	if a.State != b.State {
		return false
	}
	if a.Value == nil || b.Value == nil {
		return a.Value == nil && b.Value == nil
	}
	return *a.Value == *b.Value
}
