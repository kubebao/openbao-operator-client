package operations_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
	"github.com/kubebao/openbao-operator-client/operations"
)

func nativeControlCluster(cluster evidence.Cluster) evidence.Cluster {
	cluster.Reference.Identity.Group, cluster.Reference.APIVersion = "kubebao.org", "kubebao.org/v1alpha1"
	return cluster
}

func TestNativeControlPlansBindContractAndGroup(t *testing.T) {
	t.Parallel()
	cluster := nativeControlCluster(controlCluster(t, `,"maintenance":{"enabled":false}`))
	preparers := []func(evidence.Cluster, adapter.Contract, bool) (operations.Plan, error){
		operations.PreparePause,
		operations.PrepareMaintenance,
	}
	for _, prepare := range preparers {
		plan, err := prepare(cluster, adapter.KubeBaoV1Alpha1, true)
		if err != nil || plan.Validate() != nil || plan.Contract != adapter.KubeBaoV1Alpha1 ||
			plan.Target.Identity.Group != "kubebao.org" {
			t.Fatalf("native plan = %+v, %v", plan, err)
		}
		raw, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		var replay operations.Plan
		if err := json.Unmarshal(raw, &replay); err != nil || replay.Validate() != nil {
			t.Fatalf("native plan replay = %+v, %v", replay, err)
		}
		for _, contract := range []adapter.Contract{
			adapter.OpenBaoV1Alpha1, adapter.OpenBaoV1Alpha1Unqualified, adapter.KubeBaoV1Alpha1Unqualified, adapter.KubernetesAppsV1,
		} {
			changed := plan
			changed.Contract = contract
			if !errors.Is(changed.Validate(), operations.ErrInvalidPlan) {
				t.Fatalf("native group accepted contract %s", contract)
			}
		}
		plan.Target.Identity.Group, plan.Target.APIVersion = "openbao.org", "openbao.org/v1alpha1"
		if !errors.Is(plan.Validate(), operations.ErrInvalidPlan) {
			t.Fatal("native contract accepted legacy target identity")
		}
	}
}

func TestNativeSubmitPinsRequestAndResponse(t *testing.T) {
	t.Parallel()
	acceptedNativePause := strings.ReplaceAll(acceptedPause, "openbao.org/v1alpha1", "kubebao.org/v1alpha1")
	for _, tc := range []struct {
		name     string
		dryRun   bool
		response string
		want     operations.Outcome
	}{
		{"accepted", false, acceptedNativePause, operations.OutcomeAccepted},
		{"dry-run", true, acceptedNativePause, operations.OutcomeDryRunAccepted},
		{"legacy-response", false, acceptedPause, operations.OutcomeUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := operations.PreparePause(nativeControlCluster(controlCluster(t, "")), adapter.KubeBaoV1Alpha1, true)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			client := newControlClient(t, transportFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.Method != http.MethodPatch ||
					request.URL.Path != "/proxy/cluster-a/apis/kubebao.org/v1alpha1/namespaces/team/openbaoclusters/bao" ||
					(request.URL.Query().Get("dryRun") == "All") != tc.dryRun {
					t.Fatalf("native submission changed target: %s %s", request.Method, request.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tc.response))}, nil
			}))
			options := operations.DefaultSubmitOptions()
			options.DryRun = tc.dryRun
			result, err := client.Submit(context.Background(), plan, options)
			if err != nil || result.Outcome != tc.want || result.Requests != 1 || calls != 1 ||
				(result.Receipt != nil) != (tc.want != operations.OutcomeUnknown) {
				t.Fatalf("native result = %+v, %v, calls=%d", result, err, calls)
			}
			assertSanitizedResult(t, result, err)
		})
	}
}

func TestNativePauseAndMaintenanceAssessment(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{true, false} {
		before := nativeControlCluster(assessmentCluster(t, assessmentTime, 1, !enabled, false))
		plan, err := operations.PreparePause(before, adapter.KubeBaoV1Alpha1, enabled)
		if err != nil {
			t.Fatal(err)
		}
		observed := nativeControlCluster(assessmentCluster(t, assessmentTime.Add(time.Second), 2, enabled, false))
		assessment, err := operations.AssessPause(plan, observed, assessmentPolicy())
		if err != nil || assessment.State != operations.AssessmentObserved {
			t.Fatalf("native pause=%t: %+v, %v", enabled, assessment, err)
		}
		before = nativeControlCluster(assessmentCluster(t, assessmentTime, 1, false, !enabled))
		plan, err = operations.PrepareMaintenance(before, adapter.KubeBaoV1Alpha1, enabled)
		if err != nil {
			t.Fatal(err)
		}
		observed = nativeControlCluster(assessmentCluster(t, assessmentTime.Add(time.Second), 2, false, enabled))
		related := assessmentRelated(t, observed, enabled)
		assessment, err = operations.AssessMaintenance(plan, observed, related, assessmentPolicy())
		if err != nil || assessment.State != operations.AssessmentObserved {
			t.Fatalf("native maintenance=%t: %+v, %v", enabled, assessment, err)
		}
		// A same-name, same-UID legacy owner remains a conflicting API identity.
		related.StatefulSets.Observations[0].Metadata.Owners.Values[0].APIVersion = "openbao.org/v1alpha1"
		assessment, err = operations.AssessMaintenance(plan, observed, related, assessmentPolicy())
		if err != nil || assessment.State != operations.AssessmentInsufficient {
			t.Fatalf("legacy owner confirmed native propagation: %+v, %v", assessment, err)
		}
	}
}
