package diagnose

import (
	"cmp"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/kubebao/openbao-operator-client/evidence"
)

const (
	// maxSubsubjectLength bounds the optional finding discriminator.
	maxSubsubjectLength = 512
	// maxSummaryLength bounds the library-authored finding summary.
	maxSummaryLength = 1024
)

// RuleID is the stable identifier of a diagnostic rule.
type RuleID string

const (
	// RuleAvailability reports Available=False for the current cluster generation.
	RuleAvailability RuleID = "KBAO-CLUSTER-001"
	// RuleSealed reports OpenBaoSealed=True for the current generation of an unpaused cluster.
	RuleSealed RuleID = "KBAO-CLUSTER-002"
	// RuleReadiness reports fewer ready cluster replicas than desired after the startup grace period.
	RuleReadiness RuleID = "KBAO-CLUSTER-003"
	// RuleWorkload reports UID-associated StatefulSets with fewer ready replicas than desired,
	// or no associated StatefulSet in the declared workload scope.
	RuleWorkload RuleID = "KBAO-WORKLOAD-001"
	// RulePod reports failed or unready UID-associated Pods, or fewer captured Pods than desired.
	RulePod RuleID = "KBAO-POD-001"
	// RuleRestriction reports requested pause or maintenance, a restore hold, and active break-glass.
	RuleRestriction RuleID = "KBAO-RESTRICTION-001"
	// RuleRestore reports UID-confirmed restore requests that are in progress or require investigation.
	RuleRestore RuleID = "KBAO-RESTORE-001"
	// RuleRecovery is a reserved identifier. No supported rule implements it.
	RuleRecovery RuleID = "KBAO-RECOVERY-001"
	// RuleUpgrade reports a recorded rolling-upgrade failure.
	RuleUpgrade RuleID = "KBAO-UPGRADE-001"
	// RuleBackupFailed reports consecutive backup failures.
	RuleBackupFailed RuleID = "KBAO-BACKUP-001"
	// RuleBackupAbsent reports a configured backup without a recorded successful completion.
	RuleBackupAbsent RuleID = "KBAO-BACKUP-002"
	// RuleBackupAge reports a recorded backup completion older than the policy limit.
	RuleBackupAge RuleID = "KBAO-BACKUP-003"
	// RuleRestoreTest reports a failed restore test or an adverse restore-test condition.
	RuleRestoreTest RuleID = "KBAO-RESTORETEST-001"
	// RuleReplication is a reserved identifier. No supported rule implements it.
	RuleReplication RuleID = "KBAO-REPLICATION-001"
	// RuleCondition reports recognized adverse cluster conditions for the current generation.
	RuleCondition RuleID = "KBAO-CONDITION-001"
	// RuleController reports recorded workload and administrative-operation controller errors.
	RuleController RuleID = "KBAO-CONTROLLER-001"
	// RuleGeneration reports stale or inconsistent generation acknowledgement.
	RuleGeneration RuleID = "KBAO-GENERATION-001"
)

// Valid reports whether r is a defined rule identifier, including reserved identifiers.
func (r RuleID) Valid() bool {
	switch r {
	case RuleAvailability,
		RuleSealed,
		RuleReadiness,
		RuleWorkload,
		RulePod,
		RuleRestriction,
		RuleRestore,
		RuleRecovery,
		RuleUpgrade,
		RuleBackupFailed,
		RuleBackupAbsent,
		RuleBackupAge,
		RuleRestoreTest,
		RuleReplication,
		RuleCondition,
		RuleController,
		RuleGeneration:
		return true
	default:
		return false
	}
}

// Severity ranks a finding. Consumers own the mapping to their own severity scales.
type Severity string

const (
	// SeverityInfo marks an informational finding.
	SeverityInfo Severity = "info"
	// SeverityWarning marks a finding that warrants investigation.
	SeverityWarning Severity = "warning"
	// SeverityError marks a finding that requires action.
	SeverityError Severity = "error"
	// SeverityCritical marks a finding that indicates an outage or sealed cluster.
	SeverityCritical Severity = "critical"
)

// Rank orders severities from 1 for SeverityInfo to 4 for SeverityCritical.
// It returns 0 for an undefined severity.
func (s Severity) Rank() int {
	switch s {
	case SeverityInfo:
		return 1
	case SeverityWarning:
		return 2
	case SeverityError:
		return 3
	case SeverityCritical:
		return 4
	default:
		return 0
	}
}

var (
	// ErrInvalidFinding reports a finding that fails validation.
	ErrInvalidFinding = errors.New("invalid diagnostic finding")
	// ErrDuplicateFinding reports two findings with the same identity.
	ErrDuplicateFinding = errors.New("duplicate finding identity")
)

// Finding identity excludes severity, text, timestamps, policy, and resource versions.
// Summary is library-authored text. References identify the facts used by the rule.
type Finding struct {
	Rule       RuleID               `json:"rule"`
	Subject    evidence.Identity    `json:"subject"`
	Subsubject string               `json:"subsubject"`
	Severity   Severity             `json:"severity"`
	Summary    string               `json:"summary"`
	References []evidence.Reference `json:"references"`
}

func (f Finding) parts() []string {
	return []string{
		string(f.Rule),
		f.Subject.Origin,
		f.Subject.Group,
		f.Subject.Kind,
		f.Subject.Namespace,
		f.Subject.Name,
		f.Subject.UID,
		f.Subsubject,
	}
}

// indexPath returns the validation path of one element of a named list.
func indexPath(name string, index int) string {
	return name + "[" + strconv.Itoa(index) + "]"
}

func invalidFinding(path string, cause error) error {
	return evidence.At(path, errors.Join(ErrInvalidFinding, cause))
}

func (f Finding) validateIdentity() error {
	if !f.Rule.Valid() {
		return invalidFinding("rule", nil)
	}
	if err := f.Subject.Validate(); err != nil {
		return invalidFinding("subject", err)
	}
	if f.Subsubject != "" && !evidence.ValidText(f.Subsubject, maxSubsubjectLength) {
		return invalidFinding("subsubject", nil)
	}
	return nil
}

// Key returns the canonical identity of the finding: its rule, subject, and subsubject.
// It returns ErrInvalidFinding when an identity field is invalid.
func (f Finding) Key() (string, error) {
	if err := f.validateIdentity(); err != nil {
		return "", err
	}
	// Marshaling a string slice cannot fail.
	encoded, _ := json.Marshal(f.parts())
	return string(encoded), nil
}

// Validate checks the identity, severity, summary, and that every reference is
// valid and shares the subject's origin. Failures wrap ErrInvalidFinding.
func (f Finding) Validate() error {
	if err := f.validateIdentity(); err != nil {
		return err
	}
	if f.Severity.Rank() == 0 {
		return invalidFinding("severity", nil)
	}
	if !evidence.ValidText(f.Summary, maxSummaryLength) {
		return invalidFinding("summary", nil)
	}
	for i, ref := range f.References {
		path := indexPath("references", i)
		if err := ref.Validate(); err != nil {
			return invalidFinding(path, err)
		}
		if ref.Identity.Origin != f.Subject.Origin {
			return invalidFinding(path+".identity.origin", nil)
		}
	}
	return nil
}

// CanonicalFindings returns an independent, sorted set and rejects ambiguous identities.
// Invalid findings wrap ErrInvalidFinding; repeated identities wrap ErrDuplicateFinding.
func CanonicalFindings(values []Finding) ([]Finding, error) {
	result := append([]Finding{}, values...)
	seen := map[string]bool{}
	for i, value := range result {
		path := indexPath("findings", i)
		if err := value.Validate(); err != nil {
			return nil, evidence.At(path, err)
		}
		key, err := value.Key()
		if err != nil {
			return nil, evidence.At(path, err)
		}
		if seen[key] {
			return nil, evidence.At(path, ErrDuplicateFinding)
		}
		seen[key] = true
		result[i].References = canonicalReferences(value.References)
	}
	slices.SortFunc(result, func(a, b Finding) int {
		return cmp.Or(
			cmp.Compare(b.Severity.Rank(), a.Severity.Rank()),
			slices.Compare(a.parts(), b.parts()),
		)
	})
	return result, nil
}

// canonicalReferences returns sorted copies that share no pointers with values.
func canonicalReferences(values []evidence.Reference) []evidence.Reference {
	result := append([]evidence.Reference{}, values...)
	for i, ref := range result {
		if ref.ResourceVersion.Value != nil {
			ref.ResourceVersion.Value = new(*ref.ResourceVersion.Value)
		}
		ref.CollectedAt.Value = new(ref.CollectedAt.Value.UTC())
		result[i] = ref
	}
	slices.SortFunc(result, func(a, b evidence.Reference) int {
		return slices.Compare(referenceParts(a), referenceParts(b))
	})
	return result
}

func referenceParts(ref evidence.Reference) []string {
	version := ""
	if ref.ResourceVersion.Value != nil {
		version = *ref.ResourceVersion.Value
	}
	return []string{
		ref.Identity.Origin,
		ref.Identity.Group,
		ref.Identity.Kind,
		ref.Identity.Namespace,
		ref.Identity.Name,
		ref.Identity.UID,
		ref.Field,
		ref.APIVersion,
		string(ref.ResourceVersion.State),
		version,
		ref.CollectedAt.Value.UTC().Format(time.RFC3339Nano),
	}
}
