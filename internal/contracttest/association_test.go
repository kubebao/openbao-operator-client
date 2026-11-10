package contracttest

import (
	"cmp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/evidence"
)

type identity evidence.Identity

func (id identity) valid() bool { return evidence.Identity(id).Validate() == nil }

type request struct {
	Identity         identity `json:"identity"`
	TargetName       string   `json:"targetName"`
	TargetUID        string   `json:"targetUID"`
	ExecutionUID     string   `json:"executionUID"`
	OriginRequestUID string   `json:"originRequestUID"`
	HoldOperationID  string   `json:"holdOperationID"`
	OperationID      string   `json:"operationID"`
	Contract         string   `json:"contract"`
	CreatedAt        string   `json:"createdAt"`
}

type association struct {
	State string `json:"state"`
	Basis string `json:"basis,omitempty"`
}

func associate(target identity, r request) association {
	if !target.valid() || !r.Identity.valid() {
		return association{State: "invalid"}
	}
	if target.Origin != r.Identity.Origin || target.Group != r.Identity.Group || target.Namespace != r.Identity.Namespace {
		return association{State: "foreign"}
	}
	if r.Contract != adapterContract || target.Kind != "OpenBaoCluster" || r.Identity.Kind != "OpenBaoRestore" {
		return association{State: "unconfirmed"}
	}
	bound := r.OriginRequestUID != "" && r.OriginRequestUID == r.Identity.UID
	matched := r.TargetUID == target.UID || r.ExecutionUID == target.UID || bound
	conflict := r.TargetName != "" && r.TargetName != target.Name
	conflict = conflict || (r.TargetUID != "" && r.TargetUID != target.UID) ||
		(r.ExecutionUID != "" && r.ExecutionUID != target.UID)
	if conflict {
		if matched {
			return association{State: "conflict"}
		}
		return association{State: "foreign"}
	}
	switch {
	case r.ExecutionUID == target.UID:
		return association{State: "confirmed", Basis: "status.execution.targetUID"}
	case r.TargetUID == target.UID:
		return association{State: "confirmed", Basis: "status.target.uid"}
	case bound:
		return association{State: "confirmed", Basis: "metadata.annotations[openbao.org/restore-origin]"}
	case r.TargetName == target.Name:
		return association{State: "candidate"}
	default:
		return association{State: "unconfirmed"}
	}
}

// Selection is presentation order among confirmed requests, not causality or submission acceptance.
func latest(target identity, requests []request) string {
	confirmed := []request{}
	for _, r := range requests {
		if _, err := time.Parse(time.RFC3339Nano, r.CreatedAt); err == nil && associate(target, r).State == "confirmed" {
			confirmed = append(confirmed, r)
		}
	}
	slices.SortFunc(confirmed, func(a, b request) int {
		left, _ := time.Parse(time.RFC3339Nano, a.CreatedAt)
		right, _ := time.Parse(time.RFC3339Nano, b.CreatedAt)
		return cmp.Or(right.Compare(left), strings.Compare(a.Identity.UID, b.Identity.UID))
	})
	if len(confirmed) == 0 {
		return ""
	}
	return confirmed[0].Identity.UID
}

func TestAssociations(t *testing.T) {
	t.Parallel()
	cases := fixtures[[]struct {
		Name    string      `json:"name"`
		Target  identity    `json:"target"`
		Request request     `json:"request"`
		Want    association `json:"want"`
	}](t, "associations")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) { equal(t, associate(tc.Target, tc.Request), tc.Want) })
	}
}

func TestRequestSelection(t *testing.T) {
	t.Parallel()
	cases := fixtures[[]struct {
		Name     string    `json:"name"`
		Target   identity  `json:"target"`
		Requests []request `json:"requests"`
		Want     string    `json:"want"`
	}](t, "selection")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			equal(t, latest(tc.Target, tc.Requests), tc.Want)
			slices.Reverse(tc.Requests)
			equal(t, latest(tc.Target, tc.Requests), tc.Want)
		})
	}
}
