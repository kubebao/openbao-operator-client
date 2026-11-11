package contracttest

import (
	"slices"
	"testing"

	"github.com/kubebao/openbao-operator-client/diagnose"
	"github.com/kubebao/openbao-operator-client/evidence"
)

type finding struct {
	Rule       string   `json:"rule"`
	Subject    identity `json:"subject"`
	Subsubject string   `json:"subsubject"`
	Severity   string   `json:"severity"`
}

func coreFinding(value finding) diagnose.Finding {
	return diagnose.Finding{
		Rule: diagnose.RuleID(value.Rule), Subject: evidence.Identity(value.Subject), Subsubject: value.Subsubject,
		Severity: diagnose.Severity(value.Severity), Summary: "Synthetic contract finding",
	}
}

func findingKey(value finding) string {
	key, err := coreFinding(value).Key()
	if err != nil {
		return ""
	}
	return key
}

func order(findings []finding) []string {
	values := []diagnose.Finding{}
	for _, value := range findings {
		values = append(values, coreFinding(value))
	}
	ordered, err := diagnose.CanonicalFindings(values)
	if err != nil {
		return nil
	}
	keys := []string{}
	for i, value := range ordered {
		findings[i] = finding{
			Rule:       string(value.Rule),
			Subject:    identity(value.Subject),
			Subsubject: value.Subsubject,
			Severity:   string(value.Severity),
		}
		key, err := value.Key()
		if err != nil {
			return nil
		}
		keys = append(keys, key)
	}
	return keys
}

func TestFindingIdentityAndSeverity(t *testing.T) {
	t.Parallel()
	cases := fixtures[[]struct {
		Name    string  `json:"name"`
		Finding finding `json:"finding"`
		Key     string  `json:"key"`
		Console string  `json:"console"`
		CLI     string  `json:"cli"`
	}](t, "findings")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			equal(t, findingKey(tc.Finding), tc.Key)
			equal(
				t,
				map[string]string{"info": "info", "warning": "high", "error": "high", "critical": "critical"}[tc.Finding.Severity],
				tc.Console,
			)
			equal(
				t,
				map[string]string{"info": "info", "warning": "warning", "error": "error", "critical": "error"}[tc.Finding.Severity],
				tc.CLI,
			)
			changed := tc.Finding
			changed.Severity = "info"
			equal(t, findingKey(changed), tc.Key)
			changed.Subject.UID += "-replacement"
			if findingKey(changed) == tc.Key {
				t.Fatal("replacement inherited finding identity")
			}
		})
	}
	findings := []finding{}
	for _, tc := range cases {
		findings = append(findings, tc.Finding)
	}
	want := order(slices.Clone(findings))
	slices.Reverse(findings)
	equal(t, order(findings), want)
}

type historyEvent struct {
	At        string   `json:"at"`
	Outcome   string   `json:"outcome"`
	Findings  []string `json:"findings"`
	LegacyKey string   `json:"legacyKey"`
	NewKey    string   `json:"newKey"`
}

type historyRecord struct {
	FirstSeen   string `json:"firstSeen"`
	LastChecked string `json:"lastChecked"`
	State       string `json:"state"`
}

// Product history reconciliation is a consumer responsibility. This model specifies its boundary.
func reconcile(records map[string]historyRecord, event historyEvent) bool {
	if event.LegacyKey != "" {
		if _, exists := records[event.NewKey]; event.NewKey == "" || exists {
			return false
		}
		if old, ok := records[event.LegacyKey]; ok {
			delete(records, event.LegacyKey)
			records[event.NewKey] = old
		}
	}
	for key, record := range records {
		if event.Outcome == "complete" && !slices.Contains(event.Findings, key) {
			record.State = "resolved"
			record.LastChecked = event.At
		} else if event.Outcome != "complete" && record.State != "resolved" {
			record.State = "last_known"
		}
		records[key] = record
	}
	for _, key := range event.Findings {
		record, ok := records[key]
		if !ok || record.State == "resolved" {
			record.FirstSeen = event.At
		}
		record.LastChecked, record.State = event.At, "current"
		records[key] = record
	}
	return true
}

func TestHistorySequences(t *testing.T) {
	t.Parallel()
	cases := fixtures[[]struct {
		Name   string                   `json:"name"`
		Events []historyEvent           `json:"events"`
		Want   map[string]historyRecord `json:"want"`
	}](t, "history")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			records := map[string]historyRecord{}
			for _, event := range tc.Events {
				if !reconcile(records, event) {
					t.Fatal("ambiguous history mapping")
				}
			}
			equal(t, records, tc.Want)
		})
	}
}

func TestHistoryRejectsAmbiguousAlias(t *testing.T) {
	t.Parallel()
	records := map[string]historyRecord{"old": {FirstSeen: "first"}, "new": {FirstSeen: "second"}}
	if reconcile(records, historyEvent{LegacyKey: "old", NewKey: "new"}) {
		t.Fatal("ambiguous alias overwrote history")
	}
	equal(t, records, map[string]historyRecord{"old": {FirstSeen: "first"}, "new": {FirstSeen: "second"}})
}
