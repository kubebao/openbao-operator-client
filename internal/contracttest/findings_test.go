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
	}](t, "findings")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			equal(t, findingKey(tc.Finding), tc.Key)
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
