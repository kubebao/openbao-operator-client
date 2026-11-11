package diagnose

import (
	"cmp"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/kubebao/openbao-operator-client/evidence"
)

type RuleID string

const (
	RuleAvailability RuleID = "KBAO-CLUSTER-001"
	RuleSealed       RuleID = "KBAO-CLUSTER-002"
	RuleReadiness    RuleID = "KBAO-CLUSTER-003"
	RuleWorkload     RuleID = "KBAO-WORKLOAD-001"
	RulePod          RuleID = "KBAO-POD-001"
	RuleRestriction  RuleID = "KBAO-RESTRICTION-001"
	RuleRestore      RuleID = "KBAO-RESTORE-001"
	RuleRecovery     RuleID = "KBAO-RECOVERY-001"
	RuleUpgrade      RuleID = "KBAO-UPGRADE-001"
	RuleBackupFailed RuleID = "KBAO-BACKUP-001"
	RuleBackupAbsent RuleID = "KBAO-BACKUP-002"
	RuleBackupAge    RuleID = "KBAO-BACKUP-003"
	RuleRestoreTest  RuleID = "KBAO-RESTORETEST-001"
	RuleReplication  RuleID = "KBAO-REPLICATION-001"
	RuleCondition    RuleID = "KBAO-CONDITION-001"
	RuleController   RuleID = "KBAO-CONTROLLER-001"
	RuleGeneration   RuleID = "KBAO-GENERATION-001"
)

func (r RuleID) Valid() bool {
	return slices.Contains([]RuleID{
		RuleAvailability, RuleSealed, RuleReadiness, RuleWorkload, RulePod, RuleRestriction, RuleRestore,
		RuleRecovery, RuleUpgrade, RuleBackupFailed, RuleBackupAbsent, RuleBackupAge, RuleRestoreTest,
		RuleReplication, RuleCondition, RuleController, RuleGeneration,
	}, r)
}

type Severity string

const (
	Info     Severity = "info"
	Warning  Severity = "warning"
	Error    Severity = "error"
	Critical Severity = "critical"
)

func (s Severity) Rank() int {
	return map[Severity]int{Info: 1, Warning: 2, Error: 3, Critical: 4}[s]
}

var (
	ErrInvalidFinding   = errors.New("invalid diagnostic finding")
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
		string(f.Rule), f.Subject.Origin, f.Subject.Group, f.Subject.Kind,
		f.Subject.Namespace, f.Subject.Name, f.Subject.UID, f.Subsubject,
	}
}

func validText(value string, limit int, empty bool) bool {
	if empty && value == "" {
		return true
	}
	return len(value) <= limit && (evidence.Text{State: evidence.ValueReported, Value: &value}).Validate() == nil
}

func (f Finding) Key() (string, error) {
	if !f.Rule.Valid() || f.Subject.Validate() != nil || !validText(f.Subsubject, 512, true) {
		return "", ErrInvalidFinding
	}
	encoded, err := json.Marshal(f.parts())
	return string(encoded), err
}

func (f Finding) Validate() error {
	if _, err := f.Key(); err != nil || f.Severity.Rank() == 0 || !validText(f.Summary, 1024, false) {
		return ErrInvalidFinding
	}
	for _, ref := range f.References {
		if ref.Validate() != nil || ref.Identity.Origin != f.Subject.Origin {
			return ErrInvalidFinding
		}
	}
	return nil
}

// CanonicalFindings returns an independent, sorted set and rejects ambiguous identities.
func CanonicalFindings(values []Finding) ([]Finding, error) {
	result := append([]Finding{}, values...)
	seen := map[string]bool{}
	for i, value := range result {
		if value.Validate() != nil {
			return nil, ErrInvalidFinding
		}
		key, err := value.Key()
		if err != nil {
			return nil, err
		}
		if seen[key] {
			return nil, ErrDuplicateFinding
		}
		seen[key] = true
		result[i].References = append([]evidence.Reference{}, value.References...)
		for j, ref := range result[i].References {
			if ref.ResourceVersion.Value != nil {
				version := *ref.ResourceVersion.Value
				ref.ResourceVersion.Value = &version
			}
			collected := ref.CollectedAt.Value.UTC()
			ref.CollectedAt.Value = &collected
			result[i].References[j] = ref
		}
		slices.SortFunc(result[i].References, func(a, b evidence.Reference) int {
			return slices.Compare(referenceParts(a), referenceParts(b))
		})
	}
	slices.SortFunc(result, func(a, b Finding) int {
		return cmp.Or(cmp.Compare(b.Severity.Rank(), a.Severity.Rank()), slices.Compare(a.parts(), b.parts()))
	})
	return result, nil
}

func referenceParts(ref evidence.Reference) []string {
	version := ""
	if ref.ResourceVersion.Value != nil {
		version = *ref.ResourceVersion.Value
	}
	return []string{
		ref.Identity.Origin, ref.Identity.Group, ref.Identity.Kind, ref.Identity.Namespace, ref.Identity.Name, ref.Identity.UID,
		ref.Field, ref.APIVersion, string(ref.ResourceVersion.State), version, ref.CollectedAt.Value.UTC().Format(time.RFC3339Nano),
	}
}
