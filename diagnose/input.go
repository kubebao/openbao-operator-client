package diagnose

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/kubebao/openbao-operator-client/evidence"
)

const RuleSetVersion = "7"

var (
	ErrUnsupportedVersion = errors.New("unsupported contract version")
	ErrUnknownRule        = errors.New("unknown rule")
	ErrUnavailableRule    = errors.New("rule is not implemented in this rule set")
	ErrDuplicateSubject   = errors.New("duplicate subject")
	ErrInvalidCapture     = errors.New("invalid normalized capture")
	ErrInvalidPolicy      = errors.New("invalid evaluation policy")
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
	Subject         evidence.Identity                    `json:"subject"`
	AdapterContract string                               `json:"adapterContract"`
	Observation     *evidence.Cluster                    `json:"observation,omitempty"`
	Collection      evidence.Collection                  `json:"collection"`
	Verification    *evidence.TargetVerification         `json:"verification,omitempty"`
	StatefulSets    *ResourceInput[evidence.StatefulSet] `json:"statefulSets,omitempty"`
	Pods            *ResourceInput[evidence.Pod]         `json:"pods,omitempty"`
	Restores        *ResourceInput[evidence.Restore]     `json:"restores,omitempty"`
}

// ResourceInput retains the declared read and every admitted candidate before
// association filtering. Nil inputs on ClusterInput mean no scope was requested.
type ResourceInput[T any] struct {
	AdapterContract string              `json:"adapterContract"`
	Collection      evidence.Collection `json:"collection"`
	Observations    []T                 `json:"observations"`
}

// Capture contains normalized evidence, not raw Kubernetes resources or product state.
// Consumers own decoding and input byte limits. Analyze validates typed shapes and versions.
type Capture struct {
	SchemaVersion string         `json:"schemaVersion"`
	Clusters      []ClusterInput `json:"clusters"`
}

type Policy struct {
	RuleSetVersion string   `json:"ruleSetVersion"`
	Rules          []RuleID `json:"rules"`
	// ApprovedContracts is a caller-approved provenance allowlist, not API discovery.
	// Adapters remain responsible for validating a contract before projection.
	ApprovedContracts []string `json:"approvedContracts"`
	// RestoreUIDBindingContracts is the subset whose UID-binding semantics the
	// caller selects from adapter capabilities, independently of field projection.
	RestoreUIDBindingContracts []string      `json:"restoreUIDBindingContracts,omitempty"`
	ReportHistoricalRestores   bool          `json:"reportHistoricalRestores"`
	ReferenceTime              time.Time     `json:"referenceTime"`
	MaxBackupAge               time.Duration `json:"maxBackupAge"`
	StartupGrace               time.Duration `json:"startupGrace"`
	MaxEvidenceAge             time.Duration `json:"maxEvidenceAge"`
	MaxSubjects                int           `json:"maxSubjects"`
}

func DefaultPolicy(reference time.Time, contracts []string) Policy {
	return Policy{
		RuleSetVersion: RuleSetVersion, Rules: SupportedRules(), ApprovedContracts: slices.Clone(contracts),
		ReferenceTime: reference.UTC(), MaxBackupAge: 24 * time.Hour, StartupGrace: time.Minute,
		MaxEvidenceAge: 5 * time.Minute, MaxSubjects: 5000,
		ReportHistoricalRestores: true,
	}
}

func (p Policy) validate() error {
	if p.RuleSetVersion != RuleSetVersion {
		return ErrUnsupportedVersion
	}
	if p.ReferenceTime.IsZero() || p.ReferenceTime.Year() < 1 || p.ReferenceTime.Year() > 9999 ||
		p.MaxBackupAge <= 0 || p.StartupGrace < 0 || p.MaxEvidenceAge <= 0 || p.MaxSubjects <= 0 || len(p.ApprovedContracts) == 0 {
		return ErrInvalidPolicy
	}
	contracts := map[string]bool{}
	for _, contract := range p.ApprovedContracts {
		if !validText(contract, 253, false) || contracts[contract] {
			return ErrInvalidPolicy
		}
		contracts[contract] = true
	}
	seen := map[RuleID]bool{}
	bindings := map[string]bool{}
	for _, contract := range p.RestoreUIDBindingContracts {
		if !contracts[contract] || bindings[contract] {
			return ErrInvalidPolicy
		}
		bindings[contract] = true
	}
	for _, rule := range p.Rules {
		if !rule.Valid() {
			return ErrUnknownRule
		}
		if !slices.Contains(SupportedRules(), rule) {
			return ErrUnavailableRule
		}
		if seen[rule] {
			return ErrInvalidPolicy
		}
		seen[rule] = true
	}
	return nil
}

func (c ClusterInput) validate(p Policy) error {
	if c.Subject.Validate() != nil || c.Subject.Kind != "OpenBaoCluster" || !validText(c.AdapterContract, 253, false) ||
		c.Collection.Validate() != nil {
		return ErrInvalidCapture
	}
	if !slices.Contains(p.ApprovedContracts, c.AdapterContract) {
		return ErrUnsupportedVersion
	}
	scope := c.Collection.Scope
	if scope.Origin != c.Subject.Origin || scope.Group != c.Subject.Group || scope.Resource != "openbaoclusters" ||
		(scope.Action != evidence.ReadGet && scope.Action != evidence.ReadList) ||
		scope.Action == evidence.ReadGet && scope.Namespace == "" ||
		scope.Namespace != "" && scope.Namespace != c.Subject.Namespace || scope.Name != "" && scope.Name != c.Subject.Name {
		return ErrInvalidCapture
	}
	if !c.Collection.Interval.CompletedAt.IsZero() && c.Collection.Interval.CompletedAt.After(p.ReferenceTime) {
		return ErrInvalidCapture
	}
	if c.Observation != nil {
		o := c.Observation
		if o.Validate() != nil || o.Reference.Identity != c.Subject || o.AdapterContract != c.AdapterContract ||
			strings.TrimPrefix(o.Reference.APIVersion, c.Subject.Group+"/") != scope.Version {
			return ErrInvalidCapture
		}
		if c.Collection.Outcome == evidence.CollectionComplete &&
			(c.Collection.Items == 0 || !c.Collection.Interval.Contains(*o.Reference.CollectedAt.Value)) {
			return ErrInvalidCapture
		}
	}
	if c.Verification != nil {
		if c.Verification.Validate() != nil || c.Verification.Before.Identity != c.Subject ||
			c.Verification.AdapterContract != c.AdapterContract ||
			c.Verification.Read.Interval.CompletedAt.After(p.ReferenceTime) {
			return ErrInvalidCapture
		}
		if c.Observation != nil && (!slices.Equal(referenceParts(c.Verification.Before), referenceParts(c.Observation.Reference)) ||
			!equalInteger(c.Verification.BeforeGeneration, c.Observation.Generation)) {
			return ErrInvalidCapture
		}
	}
	return c.validateRelatedInputs(p)
}

func equalInteger(a, b evidence.Integer) bool {
	return a.State == b.State && (a.Value == nil && b.Value == nil || a.Value != nil && b.Value != nil && *a.Value == *b.Value)
}
