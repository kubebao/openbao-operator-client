package diagnose_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/diagnose"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func condition(name string, status evidence.ConditionStatus, observed int64) evidence.Condition {
	reason := "REASON_CANARY"
	return evidence.Condition{
		Type:               name,
		Status:             status,
		ObservedGeneration: integer(observed),
		Reason: evidence.Text{
			State: evidence.ValueReported,
			Value: &reason,
		},
		LastTransitionAt: evidence.Timestamp{State: evidence.ValueUnknown},
	}
}

func setConditions(cluster *evidence.Cluster, values ...evidence.Condition) {
	values = append([]evidence.Condition{}, values...)
	slices.SortFunc(values, func(a, b evidence.Condition) int { return strings.Compare(a.Type, b.Type) })
	cluster.Conditions = evidence.Conditions{State: evidence.ValueReported, Values: values}
}

func flag(value bool) evidence.Boolean {
	return evidence.Boolean{State: evidence.ValueReported, Value: &value}
}

func text(value string) evidence.Text {
	return evidence.Text{State: evidence.ValueReported, Value: &value}
}

func runRule(t *testing.T, i diagnose.ClusterInput, rule diagnose.RuleID) (diagnose.Report, diagnose.Coverage) {
	t.Helper()
	p := policy(t)
	p.Rules = []diagnose.RuleID{rule}
	report, err := diagnose.Analyze(capture(i), p)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range report.Coverage {
		if entry.Rule == rule {
			return report, entry
		}
	}
	t.Fatal("selected rule has no coverage entry")
	return diagnose.Report{}, diagnose.Coverage{}
}

func TestAvailabilityRequiresResourceAndConditionAcknowledgement(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		resource, ack int64
		status        evidence.ConditionStatus
		state         diagnose.CoverageState
		findings      int
	}{
		{"current-false", 4, 4, evidence.ConditionFalse, diagnose.Evaluated, 1},
		{"current-true", 4, 4, evidence.ConditionTrue, diagnose.Evaluated, 0},
		{"unknown-status", 4, 4, evidence.ConditionUnknown, diagnose.Insufficient, 0},
		{"stale-resource", 3, 4, evidence.ConditionFalse, diagnose.Insufficient, 0},
		{"stale-condition", 4, 3, evidence.ConditionFalse, diagnose.Insufficient, 0},
		{"future-condition", 4, 5, evidence.ConditionFalse, diagnose.Insufficient, 0},
		{"zero-condition", 4, 0, evidence.ConditionFalse, diagnose.Insufficient, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := input(t)
			i.Observation.ObservedGeneration = integer(tc.resource)
			setConditions(i.Observation, condition("Available", tc.status, tc.ack))
			report, coverage := runRule(t, i, diagnose.RuleAvailability)
			if coverage.State != tc.state || len(report.Findings) != tc.findings {
				t.Fatal("availability used unconfirmed generations or status")
			}
			if len(report.Findings) > 0 && report.Findings[0].Severity != diagnose.Warning {
				t.Fatal("availability severity changed")
			}
		})
	}
}

func TestSealingUsesItsOwnAcknowledgementAndPause(t *testing.T) {
	t.Parallel()
	i := input(t)
	i.Observation.ObservedGeneration = integer(3)
	setConditions(i.Observation, condition("OpenBaoSealed", evidence.ConditionTrue, 4))
	report, coverage := runRule(t, i, diagnose.RuleSealed)
	if coverage.State != diagnose.Evaluated || len(report.Findings) != 1 || report.Findings[0].Severity != diagnose.Critical {
		t.Fatal("resource acknowledgement replaced individual condition acknowledgement")
	}
	for _, ack := range []int64{0, 3, 5, -1} {
		setConditions(i.Observation, condition("OpenBaoSealed", evidence.ConditionTrue, ack))
		report, coverage = runRule(t, i, diagnose.RuleSealed)
		if coverage.State != diagnose.Insufficient || len(report.Findings) != 0 {
			t.Fatal("noncurrent sealing became a current finding")
		}
	}
	i.Observation.Paused = flag(true)
	i.Observation.Conditions = evidence.Conditions{State: evidence.ValueUnknown}
	report, coverage = runRule(t, i, diagnose.RuleSealed)
	if coverage.State != diagnose.Evaluated || len(report.Findings) != 0 {
		t.Fatal("pause did not suppress sealing")
	}
	i.Observation.Paused = evidence.Boolean{State: evidence.ValueUnknown}
	report, coverage = runRule(t, i, diagnose.RuleSealed)
	if coverage.State != diagnose.Insufficient || len(report.Findings) != 0 {
		t.Fatal("unknown pause became known false")
	}
}

func TestGenericConditionsHaveSeparateIdentitiesAndNoSpecializedDuplicates(t *testing.T) {
	t.Parallel()
	i := input(t)
	setConditions(i.Observation,
		condition("Available", evidence.ConditionFalse, 4), condition("OpenBaoSealed", evidence.ConditionTrue, 4),
		condition("Degraded", evidence.ConditionTrue, 4), condition("TLSReady", evidence.ConditionFalse, 4),
		condition("FutureCondition", evidence.ConditionUnknown, 0))
	report, coverage := runRule(t, i, diagnose.RuleCondition)
	if coverage.State != diagnose.Evaluated || len(report.Findings) != 2 {
		t.Fatal("generic conditions duplicated specialized or unknown conditions")
	}
	seen := map[string]bool{}
	for _, found := range report.Findings {
		if found.Subsubject != "Degraded" && found.Subsubject != "TLSReady" {
			t.Fatal("wrong condition polarity or subsubject")
		}
		key, err := found.Key()
		if err != nil || seen[key] {
			t.Fatal("condition finding identities collided")
		}
		seen[key] = true
	}
	setConditions(i.Observation, condition("Degraded", evidence.ConditionTrue, 4), condition("TLSReady", evidence.ConditionFalse, 3))
	report, coverage = runRule(t, i, diagnose.RuleCondition)
	if coverage.State != diagnose.Insufficient || len(report.Findings) != 1 || report.Findings[0].Subsubject != "Degraded" {
		t.Fatal("stale optional evidence discarded an independently confirmed condition")
	}
	encoded, err := json.Marshal(report)
	if err != nil || strings.Contains(string(encoded), "CANARY") {
		t.Fatal("condition reason escaped static diagnostic text")
	}
}

func TestConditionTransitionTimeIsNotAHeartbeat(t *testing.T) {
	t.Parallel()
	i := input(t)
	value := condition("Available", evidence.ConditionFalse, 4)
	value.LastTransitionAt = timestamp(i.Observation.Reference.CollectedAt.Value.Add(-365 * 24 * time.Hour))
	setConditions(i.Observation, value)
	report, coverage := runRule(t, i, diagnose.RuleAvailability)
	if coverage.State != diagnose.Evaluated || len(report.Findings) != 1 {
		t.Fatal("old transition suppressed current reported status")
	}
	value.LastTransitionAt = timestamp(i.Observation.Reference.CollectedAt.Value.Add(time.Second))
	setConditions(i.Observation, value)
	report, coverage = runRule(t, i, diagnose.RuleAvailability)
	if coverage.State != diagnose.Insufficient || coverage.Reason != diagnose.ReasonTime || len(report.Findings) != 1 {
		t.Fatal("inconsistent transition time hid an independent status fact or became complete coverage")
	}
}

func TestGenerationDistinguishesStaleInconsistentAndUnknown(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		observed int64
		state    diagnose.CoverageState
		findings int
		word     string
	}{
		{4, diagnose.Evaluated, 0, ""},
		{3, diagnose.Evaluated, 1, "earlier"},
		{5, diagnose.Evaluated, 1, "inconsistent"},
		{-1, diagnose.Evaluated, 1, "inconsistent"},
		{0, diagnose.Insufficient, 0, ""},
	} {
		i := input(t)
		i.Observation.ObservedGeneration = integer(tc.observed)
		report, coverage := runRule(t, i, diagnose.RuleGeneration)
		if coverage.State != tc.state || len(report.Findings) != tc.findings {
			t.Fatal("generation states were flattened")
		}
		if len(report.Findings) > 0 && !strings.Contains(report.Findings[0].Summary, tc.word) {
			t.Fatal("generation distinction was lost")
		}
	}
}

func TestRestrictionsRemainIndependentAndUnknownFlagsStayIncomplete(t *testing.T) {
	t.Parallel()
	i := input(t)
	i.Observation.Paused = flag(true)
	i.Observation.Maintenance = evidence.Maintenance{State: evidence.ObjectReported, Enabled: flag(true)}
	i.Observation.RestoreHold = text("operation-1")
	i.Observation.BreakGlass = evidence.BreakGlass{
		State: evidence.ObjectReported, Active: flag(true), Reason: text("REASON_CANARY"),
		EnteredAt: evidence.Timestamp{State: evidence.ValueUnknown},
	}
	before, _ := json.Marshal(i)
	report, coverage := runRule(t, i, diagnose.RuleRestriction)
	if coverage.State != diagnose.Evaluated || len(report.Findings) != 4 {
		t.Fatal("independent restrictions were combined")
	}
	warnings, infos := 0, 0
	keys := map[string]bool{}
	for _, found := range report.Findings {
		if found.Subsubject == "" || found.Subsubject != found.References[0].Field {
			t.Fatal("restriction identity did not use its field")
		}
		key, err := found.Key()
		if err != nil || keys[key] {
			t.Fatal("restriction finding identities collided")
		}
		keys[key] = true
		if found.Severity == diagnose.Warning {
			warnings++
		}
		if found.Severity == diagnose.Info {
			infos++
		}
	}
	if warnings != 1 || infos != 3 {
		t.Fatal("restriction severities changed")
	}
	after, _ := json.Marshal(i)
	if string(before) != string(after) {
		t.Fatal("diagnosis changed restriction intent")
	}
	i.Observation.Maintenance.Enabled = evidence.Boolean{State: evidence.ValueUnknown}
	report, coverage = runRule(t, i, diagnose.RuleRestriction)
	if coverage.State != diagnose.Insufficient || len(report.Findings) != 3 {
		t.Fatal("unknown maintenance erased independent restrictions")
	}
	i.Observation.Paused = flag(false)
	i.Observation.Maintenance.Enabled = flag(false)
	i.Observation.BreakGlass.Active = flag(false)
	report, coverage = runRule(t, i, diagnose.RuleRestriction)
	if coverage.State != diagnose.Evaluated || len(report.Findings) != 1 ||
		report.Findings[0].Subsubject != "metadata.annotations[openbao.org/restore-hold]" {
		t.Fatal("independent restore hold was cleared by false pause, maintenance, or break-glass flags")
	}
	encoded, err := json.Marshal(report)
	if err != nil || strings.Contains(string(encoded), "CANARY") {
		t.Fatal("break-glass reason escaped diagnostic text")
	}
}

func TestControllerErrorsRemainHistoricalAndSubjectSpecific(t *testing.T) {
	t.Parallel()
	i := input(t)
	i.Observation.WorkloadError = evidence.ControllerError{
		State: evidence.ObjectReported, Reason: text("WORKLOAD_CANARY"),
		At: timestamp(i.Observation.Reference.CollectedAt.Value.Add(-time.Hour)),
	}
	i.Observation.AdminOpsError = evidence.ControllerError{
		State: evidence.ObjectReported, Reason: text("ADMIN_CANARY"),
		At: evidence.Timestamp{State: evidence.ValueUnknown},
	}
	report, coverage := runRule(t, i, diagnose.RuleController)
	if coverage.State != diagnose.Evaluated || len(report.Findings) != 2 || report.Findings[0].Subsubject == report.Findings[1].Subsubject {
		t.Fatal("controller domains lost their identities")
	}
	for _, found := range report.Findings {
		if !strings.Contains(found.Summary, "records") {
			t.Fatal("historical error became an active failure claim")
		}
	}
	i.Observation.AdminOpsError.Reason = evidence.Text{State: evidence.ValueInvalid}
	report, coverage = runRule(t, i, diagnose.RuleController)
	if coverage.State != diagnose.Insufficient || len(report.Findings) != 1 {
		t.Fatal("malformed optional domain erased independent error")
	}
	encoded, err := json.Marshal(report)
	if err != nil || strings.Contains(string(encoded), "CANARY") {
		t.Fatal("controller provider text escaped projection")
	}
}

func TestCurrentRuleInventoryRejectsUnknownRules(t *testing.T) {
	t.Parallel()
	p := policy(t)
	p.Rules = append(p.Rules, diagnose.RuleID("KBAO-UNKNOWN-001"))
	if _, err := diagnose.Analyze(capture(input(t)), p); !errors.Is(err, diagnose.ErrUnknownRule) {
		t.Fatal("unknown rule accepted")
	}
	if len(diagnose.SupportedRules()) != 15 {
		t.Fatal("unexpected executable inventory")
	}
}

func TestReadReplicaConditionsApplyOnlyToConfiguredReadReplicas(t *testing.T) {
	t.Parallel()
	notConfigured := func(name string, status evidence.ConditionStatus) evidence.Condition {
		value := condition(name, status, 4)
		value.Reason = text("NoReadReplicasConfigured")
		return value
	}
	conditions := []evidence.Condition{
		notConfigured("ReadReplicaStorageConfigured", evidence.ConditionFalse), notConfigured("ReadReplicasReady", evidence.ConditionFalse),
		notConfigured("ReadServingAvailable", evidence.ConditionFalse), condition("TLSReady", evidence.ConditionFalse, 4),
	}
	for _, tc := range []struct {
		name  string
		pool  func(*evidence.ReplicaCounts)
		want  []string
		other bool
	}{
		{"absent pool", func(p *evidence.ReplicaCounts) { p.Configuration = evidence.ObjectAbsent }, []string{"TLSReady"}, false},
		{"zero replicas", func(p *evidence.ReplicaCounts) {
			p.Configuration, p.State, p.Desired = evidence.ObjectReported, evidence.ObjectAbsent, integer(0)
		}, []string{"TLSReady"}, false},
		{"requested replicas", func(p *evidence.ReplicaCounts) {
			p.Configuration, p.State, p.Desired = evidence.ObjectReported, evidence.ObjectAbsent, integer(2)
		}, []string{"ReadReplicaStorageConfigured", "ReadReplicasReady", "ReadServingAvailable", "TLSReady"}, false},
		{"unknown pool and operator reason", func(p *evidence.ReplicaCounts) {
			p.Configuration, p.State = evidence.ObjectUnknown, evidence.ObjectUnknown
		}, []string{"TLSReady"}, false},
		{"unknown pool and another reason", func(p *evidence.ReplicaCounts) {
			p.Configuration, p.State = evidence.ObjectUnknown, evidence.ObjectUnknown
		}, []string{"ReadReplicasReady", "TLSReady"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			i := input(t)
			tc.pool(&i.Observation.ReadReplicas)
			values := append([]evidence.Condition{}, conditions...)
			if tc.other {
				values = []evidence.Condition{condition("ReadReplicasReady", evidence.ConditionFalse, 4), conditions[3]}
			}
			setConditions(i.Observation, values...)
			if i.Observation.Validate() != nil {
				t.Fatal("invalid test cluster")
			}
			report, coverage := runRule(t, i, diagnose.RuleCondition)
			got := []string{}
			for _, found := range report.Findings {
				got = append(got, found.Subsubject)
			}
			slices.Sort(got)
			if coverage.State != diagnose.Evaluated || !slices.Equal(got, tc.want) {
				t.Fatalf("findings for %v, want %v (coverage %s)", got, tc.want, coverage.State)
			}
		})
	}
}

func TestConditionFindingsNameTheConditionAndEtcdIsAdvisory(t *testing.T) {
	t.Parallel()
	i := input(t)
	setConditions(i.Observation, condition("EtcdEncryptionWarning", evidence.ConditionTrue, 4),
		condition("TLSReady", evidence.ConditionFalse, 4))
	report, _ := runRule(t, i, diagnose.RuleCondition)
	if len(report.Findings) != 2 {
		t.Fatalf("findings = %+v", report.Findings)
	}
	for _, found := range report.Findings {
		switch found.Subsubject {
		case "EtcdEncryptionWarning":
			if found.Severity != diagnose.Info || !strings.Contains(found.Summary, "encrypts Secrets at rest in etcd") {
				t.Fatalf("etcd advisory = %+v", found)
			}
		case "TLSReady":
			if found.Severity != diagnose.Warning || found.Summary != "The operator reports TLSReady=False for the current generation." {
				t.Fatalf("condition finding = %+v", found)
			}
		default:
			t.Fatalf("unexpected finding %+v", found)
		}
		if strings.Contains(found.Summary, "CANARY") {
			t.Fatal("a condition reason reached finding text")
		}
	}
}

func TestRecognizedConditionMatchesTheRuleInventory(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{
		"TLSReady": true, "EtcdEncryptionWarning": true, "UserAccessBootstrap": true, "ReadReplicasReady": true,
		"Available": false, "OpenBaoSealed": false, "FutureCondition": false,
	} {
		if diagnose.RecognizedCondition(name) != want {
			t.Fatalf("RecognizedCondition(%q) != %v", name, want)
		}
	}
}
