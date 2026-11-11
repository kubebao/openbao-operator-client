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

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/diagnose"
	"github.com/kubebao/openbao-operator-client/evidence"
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

// Replay translates the draft fixture envelope into normalized production input.
// Byte decoding is test-boundary code; policy, findings, and coverage use diagnose.
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
	if input.AdapterContract != adapterContract {
		return result, diagnose.ErrUnsupportedVersion
	}
	reference, err := time.Parse(time.RFC3339Nano, policy.Reference)
	if err != nil {
		return result, diagnose.ErrInvalidPolicy
	}
	actualPolicy := diagnose.DefaultPolicy(reference, []string{adapterContract})
	actualPolicy.RuleSetVersion = policy.RuleVersion
	actualPolicy.Rules = []diagnose.RuleID{}
	for _, rule := range policy.Rules {
		actualPolicy.Rules = append(actualPolicy.Rules, diagnose.RuleID(rule))
	}
	actualPolicy.MaxBackupAge = time.Duration(policy.BackupAge) * time.Second
	actualPolicy.StartupGrace = time.Duration(policy.StartupGrace) * time.Second
	actual := diagnose.Capture{SchemaVersion: evidence.SchemaVersion, Clusters: []diagnose.ClusterInput{}}
	if input.EvidenceVersion != "1" {
		actual.SchemaVersion = input.EvidenceVersion
	}
	collected := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	for _, o := range input.Observations {
		spec := map[string]any{"replicas": o.Desired, "paused": o.Paused}
		switch {
		case o.Backup == nil:
			spec["backup"] = nil
		case *o.Backup:
			spec["backup"] = map[string]any{}
		}
		raw, err := json.Marshal(map[string]any{
			"apiVersion": "openbao.org/v1alpha1", "kind": "OpenBaoCluster",
			"metadata": map[string]any{
				"namespace": o.Identity.Namespace, "name": o.Identity.Name, "uid": o.Identity.UID,
				"generation": o.Generation, "creationTimestamp": o.Created,
			},
			"spec": spec, "status": map[string]any{
				"observedGeneration": o.Observed, "readyReplicas": o.Ready,
				"backup": map[string]any{"consecutiveFailures": 0, "lastBackupTime": o.Success},
			},
		})
		if err != nil {
			return result, err
		}
		options := fixtureOptions(o.Identity.Origin, adapter.V1Alpha1Snapshot)
		options.CollectedAt = collected
		cluster, err := adapter.ProjectCluster(raw, options)
		if err != nil {
			return result, err
		}
		scope := evidence.Scope{
			Origin: o.Identity.Origin, Group: o.Identity.Group, Version: "v1alpha1", Resource: "openbaoclusters",
			Namespace: o.Identity.Namespace, Name: o.Identity.Name, Action: evidence.ReadGet,
		}
		actual.Clusters = append(actual.Clusters, diagnose.ClusterInput{
			Subject: evidence.Identity(o.Identity), AdapterContract: adapterContract,
			Observation: &cluster, Collection: evidence.Collection{
				Scope: scope, Outcome: evidence.CollectionComplete, Items: 1,
				Interval: evidence.Interval{StartedAt: collected, CompletedAt: collected},
			},
		})
	}
	report, err := diagnose.Analyze(actual, actualPolicy)
	if err != nil {
		return result, err
	}
	for _, value := range report.Findings {
		result.Findings = append(result.Findings, finding{
			Rule: string(value.Rule), Subject: identity(value.Subject),
			Subsubject: value.Subsubject, Severity: string(value.Severity),
		})
	}
	for _, value := range report.Coverage {
		result.Coverage = append(
			result.Coverage,
			ruleCoverage{Rule: string(value.Rule), Subject: identity(value.Subject), State: string(value.State)},
		)
	}
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
