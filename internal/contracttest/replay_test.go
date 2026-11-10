package contracttest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/evidence"
)

const (
	readinessRule = "KBAO-CLUSTER-003"
	backupAgeRule = "KBAO-BACKUP-003"
)

type observation struct {
	Identity   identity `json:"identity"`
	Generation *int64   `json:"generation"`
	Observed   *int64   `json:"observed"`
	Desired    *int64   `json:"desired"`
	Ready      *int64   `json:"ready"`
	Paused     *bool    `json:"paused"`
	Created    string   `json:"created"`
	Backup     *bool    `json:"backup"`
	Success    string   `json:"success"`
}

type capture struct {
	EvidenceVersion string        `json:"evidenceVersion"`
	AdapterContract string        `json:"adapterContract"`
	Observations    []observation `json:"observations"`
}

type evaluationPolicy struct {
	RuleVersion  string   `json:"ruleVersion"`
	Rules        []string `json:"rules"`
	Reference    string   `json:"reference"`
	BackupAge    int      `json:"backupAge"`
	StartupGrace int      `json:"startupGrace"`
}

type ruleCoverage struct {
	Rule    string   `json:"rule"`
	Subject identity `json:"subject"`
	State   string   `json:"state"`
}

type analysis struct {
	Findings []finding      `json:"findings"`
	Coverage []ruleCoverage `json:"coverage"`
}

func readyFinding(o observation, p evaluationPolicy, reference time.Time) (string, string) {
	created, err := time.Parse(time.RFC3339Nano, o.Created)
	if err != nil || created.After(reference) || o.Paused == nil || o.Desired == nil || o.Ready == nil ||
		*o.Desired < 0 || *o.Ready < 0 || evidence.CompareGeneration(o.Generation, o.Observed) != evidence.GenerationCurrent {
		return "insufficient", ""
	}
	if *o.Paused || reference.Sub(created) < time.Duration(p.StartupGrace)*time.Second || *o.Desired == 0 || *o.Ready >= *o.Desired {
		return "evaluated", ""
	}
	if *o.Ready == 0 {
		return "evaluated", "critical"
	}
	return "evaluated", "warning"
}

func backupFinding(o observation, p evaluationPolicy, reference time.Time) (string, string) {
	if o.Backup == nil {
		return "insufficient", ""
	}
	if !*o.Backup {
		return "not_applicable", ""
	}
	success, err := time.Parse(time.RFC3339Nano, o.Success)
	if err != nil || success.After(reference) {
		return "insufficient", ""
	}
	if reference.Sub(success) > time.Duration(p.BackupAge)*time.Second {
		return "evaluated", "warning"
	}
	return "evaluated", ""
}

// Replay exercises representative time-dependent rules, not the complete production rule catalog.
func replay(data []byte, policy evaluationPolicy) (analysis, error) {
	result := analysis{Findings: []finding{}, Coverage: []ruleCoverage{}}
	var input capture
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return result, fmt.Errorf("invalid capture: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return result, errors.New("trailing capture content")
	}
	if input.EvidenceVersion != "1" || input.AdapterContract != adapterContract || policy.RuleVersion != "1" {
		return result, errors.New("unsupported contract version")
	}
	reference, err := time.Parse(time.RFC3339Nano, policy.Reference)
	if err != nil || policy.BackupAge <= 0 || policy.StartupGrace < 0 {
		return result, errors.New("invalid evaluation policy")
	}
	for _, rule := range policy.Rules {
		if rule != readinessRule && rule != backupAgeRule {
			return result, fmt.Errorf("unknown rule %s", rule)
		}
	}
	seen := map[identity]bool{}
	for _, o := range input.Observations {
		if !o.Identity.valid() || seen[o.Identity] {
			return result, errors.New("invalid or duplicate subject")
		}
		seen[o.Identity] = true
		for _, rule := range []string{readinessRule, backupAgeRule} {
			state, severity := "excluded", ""
			if slices.Contains(policy.Rules, rule) {
				if rule == readinessRule {
					state, severity = readyFinding(o, policy, reference)
				} else {
					state, severity = backupFinding(o, policy, reference)
				}
			}
			result.Coverage = append(result.Coverage, ruleCoverage{Rule: rule, Subject: o.Identity, State: state})
			if severity != "" {
				result.Findings = append(result.Findings, finding{Rule: rule, Subject: o.Identity, Severity: severity})
			}
		}
	}
	order(result.Findings)
	slices.SortFunc(result.Coverage, func(a, b ruleCoverage) int {
		return slices.Compare(
			findingParts(finding{Rule: a.Rule, Subject: a.Subject}),
			findingParts(finding{Rule: b.Rule, Subject: b.Subject}),
		)
	})
	return result, nil
}

func TestSerializedReplay(t *testing.T) {
	t.Parallel()
	cases := fixtures[[]struct {
		Name    string           `json:"name"`
		Capture capture          `json:"capture"`
		Policy  evaluationPolicy `json:"policy"`
		Want    analysis         `json:"want"`
		Error   string           `json:"error"`
	}](t, "replay")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			data, err := json.Marshal(tc.Capture)
			if err != nil {
				t.Fatal(err)
			}
			got, err := replay(data, tc.Policy)
			if tc.Error != "" {
				if err == nil || !strings.Contains(err.Error(), tc.Error) {
					t.Fatalf("error = %v, want %s", err, tc.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			equal(t, got, tc.Want)
			first, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			slices.Reverse(tc.Capture.Observations)
			slices.Reverse(tc.Policy.Rules)
			data, err = json.Marshal(tc.Capture)
			if err != nil {
				t.Fatal(err)
			}
			again, err := replay(data, tc.Policy)
			if err != nil {
				t.Fatal(err)
			}
			second, err := json.Marshal(again)
			if err != nil {
				t.Fatal(err)
			}
			equal(t, string(second), string(first))
		})
	}
}

func TestReplayProcessBoundary(t *testing.T) {
	t.Parallel()
	policy := evaluationPolicy{RuleVersion: "1", Reference: "2026-11-10T10:00:00Z", BackupAge: 3600}
	for _, field := range []string{"exitCode", "resultCode", "failureThreshold"} {
		data := []byte(`{"evidenceVersion":"1","adapterContract":"` + adapterContract + `","` + field + `":1}`)
		if _, err := replay(data, policy); err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("process field %s accepted: %v", field, err)
		}
	}
	data := []byte(`{"evidenceVersion":"1","adapterContract":"` + adapterContract + `"}`)
	if _, err := replay(append(data, []byte(`{}`)...), policy); err == nil {
		t.Fatal("trailing capture accepted")
	}
}
