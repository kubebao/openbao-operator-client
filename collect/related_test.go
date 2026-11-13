package collect_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/collect"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func relatedRequest(contract adapter.Contract, group, version, resource string) *collect.Request {
	r := request()
	r.Contract, r.Scope.Group, r.Scope.Version, r.Scope.Resource = contract, group, version, resource
	return &r
}

func relatedPlan() collect.RelatedPlan {
	return collect.RelatedPlan{
		StatefulSets: relatedRequest(adapter.KubernetesAppsV1, "apps", "v1", "statefulsets"),
		Pods:         relatedRequest(adapter.KubernetesCoreV1, "", "v1", "pods"),
		Restores:     relatedRequest(adapter.OpenBaoV1Alpha1, "openbao.org", "v1alpha1", "openbaorestores"),
		Verify:       verifyRequest(), Limits: collect.DefaultLimits(),
	}
}

func relatedPage(resource string) string {
	apiVersion, kind := "openbao.org/v1alpha1", "OpenBaoRestore"
	item := `{"metadata":{"namespace":"team","name":"restore","uid":"restore-1"},` +
		`"spec":{"cluster":"bao","source":{"token":"SPEC_CANARY"}},"status":{"target":{"uid":"cluster-1"}}}`
	switch resource {
	case "statefulsets":
		apiVersion, kind = "apps/v1", "StatefulSet"
		item = `{"metadata":{"namespace":"team","name":"bao","uid":"set-1","ownerReferences":[` +
			`{"apiVersion":"openbao.org/v1alpha1","kind":"OpenBaoCluster","name":"bao","uid":"cluster-1","controller":true}]}}`
	case "pods":
		apiVersion, kind = "v1", "Pod"
		item = `{"metadata":{"namespace":"team","name":"bao-0","uid":"pod-1","ownerReferences":[` +
			`{"apiVersion":"apps/v1","kind":"StatefulSet","name":"bao","uid":"set-1","controller":true}]}}`
	}
	return fmt.Sprintf(`{"apiVersion":%q,"kind":%q,"metadata":{"resourceVersion":"10"},"items":[%s]}`, apiVersion, kind+"List", item)
}

func TestRelatedCollectionUsesDeclaredScopesAndFinalVerification(t *testing.T) {
	t.Parallel()
	paths := []string{}
	client := testClient(t, func(writer http.ResponseWriter, incoming *http.Request) {
		paths = append(paths, incoming.URL.Path)
		if strings.HasSuffix(incoming.URL.Path, "/openbaoclusters/bao") {
			writeBody(t, writer, targetDocument("cluster-1", `,"generation":1`))
			return
		}
		parts := strings.Split(incoming.URL.Path, "/")
		writeBody(t, writer, relatedPage(parts[len(parts)-1]))
	})
	result, err := client.Related(context.Background(), selectedTarget(t), relatedPlan())
	if err != nil || result.Outcome != evidence.CollectionComplete || result.Requests != 4 ||
		result.Verification.State != evidence.VerificationVerified || result.Verification.Validate() != nil ||
		result.Interval.Validate() != nil || len(result.Associations) != 3 {
		t.Fatalf("outcome=%s verification=%s error=%v", result.Outcome, result.Verification.State, err)
	}
	for _, association := range result.Associations {
		if association.Validate() != nil || association.State != evidence.AssociationConfirmed {
			t.Fatal("declared ownership chain was not confirmed")
		}
	}
	if len(paths) != 4 || paths[0] != "/prefix/apis/apps/v1/namespaces/team/statefulsets" ||
		paths[1] != "/prefix/api/v1/namespaces/team/pods" ||
		paths[2] != "/prefix/apis/openbao.org/v1alpha1/namespaces/team/openbaorestores" ||
		paths[3] != "/prefix/apis/openbao.org/v1alpha1/namespaces/team/openbaoclusters/bao" {
		t.Fatalf("unexpected read sequence: %v", paths)
	}
	encoded, err := json.Marshal(result)
	if err != nil || strings.Contains(string(encoded), "CANARY") {
		t.Fatal("related source credentials escaped projection")
	}
}

func TestRelatedRetainsIndependentFactsAfterDeniedReadAndReplacement(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(writer http.ResponseWriter, incoming *http.Request) {
		switch {
		case strings.HasSuffix(incoming.URL.Path, "/statefulsets"):
			writer.WriteHeader(http.StatusForbidden)
		case strings.HasSuffix(incoming.URL.Path, "/pods"):
			writeBody(t, writer, relatedPage("pods"))
		case strings.HasSuffix(incoming.URL.Path, "/openbaorestores"):
			writeBody(t, writer, relatedPage("openbaorestores"))
		default:
			writeBody(t, writer, targetDocument("cluster-2", `,"generation":1`))
		}
	})
	result, err := client.Related(context.Background(), selectedTarget(t), relatedPlan())
	if err != nil || result.Outcome != evidence.CollectionReplaced || result.Verification.Validate() != nil ||
		result.StatefulSets.Collection.Outcome != evidence.CollectionDenied || len(result.Pods.Observations) != 1 ||
		len(result.Restores.Observations) != 1 || result.Verification.Before.Identity.UID != "cluster-1" {
		t.Fatal("incomplete or replaced target discarded independent evidence or changed its identity")
	}
}

func TestRelatedBudgetsReserveVerificationAndOmitUnrequestedScopes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		change   func(*collect.RelatedPlan)
		requests int
		outcome  evidence.CollectionOutcome
	}{
		{"restore-only", func(p *collect.RelatedPlan) { p.StatefulSets, p.Pods = nil, nil }, 2, evidence.CollectionComplete},
		{"one-request", func(p *collect.RelatedPlan) { p.Limits.MaxPages = 1 }, 1, evidence.CollectionPageLimit},
		{
			"one-candidate", func(p *collect.RelatedPlan) { p.Limits.MaxCandidates, p.Limits.PageSize = 1, 1 },
			2, evidence.CollectionSizeLimit,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, func(writer http.ResponseWriter, incoming *http.Request) {
				if strings.HasSuffix(incoming.URL.Path, "/openbaoclusters/bao") {
					writeBody(t, writer, targetDocument("cluster-1", `,"generation":1`))
					return
				}
				parts := strings.Split(incoming.URL.Path, "/")
				writeBody(t, writer, relatedPage(parts[len(parts)-1]))
			})
			plan := relatedPlan()
			tc.change(&plan)
			result, err := client.Related(context.Background(), selectedTarget(t), plan)
			if err != nil || result.Requests != tc.requests || result.Outcome != tc.outcome ||
				result.Verification.State != evidence.VerificationVerified || result.Verification.Validate() != nil {
				t.Fatalf("outcome=%s requests=%d error=%v", result.Outcome, result.Requests, err)
			}
			if tc.name == "restore-only" && (result.StatefulSets != nil || result.Pods != nil) {
				t.Fatal("unrequested scopes acquired evidence or collection results")
			}
		})
	}
}

func TestRelatedChargesFailedBodiesToSharedBudget(t *testing.T) {
	t.Parallel()
	body := &countedBody{Reader: strings.NewReader(strings.Repeat("ERROR_CANARY", 100))}
	client, err := collect.NewClient("https://example.test", roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, ContentLength: -1, Body: body}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	plan := relatedPlan()
	plan.Limits.MaxBytes, plan.Limits.MaxResponseBytes = 32, 32
	result, err := client.Related(context.Background(), selectedTarget(t), plan)
	if err != nil || result.Outcome != evidence.CollectionSizeLimit || result.Bytes != 33 || result.Requests != 1 ||
		!body.closed || result.Verification.Read.Outcome != evidence.CollectionNotCollected || result.Verification.Validate() != nil {
		t.Fatal("failed response bytes escaped the aggregate budget or missing verification was treated as complete")
	}
}

func TestRelatedHasOneDeadlineAndRejectsInvalidPlansBeforeIO(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(_ http.ResponseWriter, incoming *http.Request) { <-incoming.Context().Done() })
	plan := relatedPlan()
	plan.Limits.Timeout = 10 * time.Millisecond
	result, err := client.Related(context.Background(), selectedTarget(t), plan)
	if err != nil || result.Outcome != evidence.CollectionTimeout || result.Requests != 1 || result.Verification.Validate() != nil {
		t.Fatal("shared deadline did not bound all reads and final verification")
	}
	noIO, err := collect.NewClient("https://example.test", roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("invalid plan reached transport")
		return nil, io.EOF
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*collect.RelatedPlan){
		func(p *collect.RelatedPlan) { p.Pods.Scope.Namespace = "other" },
		func(p *collect.RelatedPlan) { p.Verify.ExpectedUID = "other" },
		func(p *collect.RelatedPlan) {
			p.Restores.Contract = adapter.KubeBaoV1Alpha1
			p.Restores.Scope.Group = "kubebao.org"
		},
		func(p *collect.RelatedPlan) { p.StatefulSets, p.Pods, p.Restores = nil, nil, nil },
	} {
		plan := relatedPlan()
		change(&plan)
		if _, err := noIO.Related(context.Background(), selectedTarget(t), plan); !errors.Is(err, collect.ErrInvalidRequest) {
			t.Fatal("invalid plan was accepted")
		}
	}
	target := selectedTarget(t)
	target.Reference.APIVersion = "openbao.org/v2"
	if _, err := noIO.Related(context.Background(), target, relatedPlan()); !errors.Is(err, collect.ErrInvalidRequest) {
		t.Fatal("target API version was silently changed during verification")
	}
}

func TestRelatedReadCompletionIsIndependentOfVerificationAndAssociation(t *testing.T) {
	t.Parallel()
	for _, changed := range []bool{false, true} {
		client := testClient(t, func(writer http.ResponseWriter, incoming *http.Request) {
			if strings.HasSuffix(incoming.URL.Path, "/openbaoclusters/bao") {
				generation := `,"generation":1`
				if changed {
					generation = `,"generation":2`
				}
				writeBody(t, writer, targetDocument("cluster-1", generation))
				return
			}
			page := relatedPage("openbaorestores")
			if !changed {
				page = strings.Replace(page, `"target":{"uid":"cluster-1"}`, `"target":{}`, 1)
			}
			writeBody(t, writer, page)
		})
		plan := relatedPlan()
		plan.StatefulSets, plan.Pods = nil, nil
		result, err := client.Related(context.Background(), selectedTarget(t), plan)
		if err != nil || result.Outcome != evidence.CollectionComplete || len(result.Restores.Observations) != 1 ||
			result.Verification.Validate() != nil {
			t.Fatal("interpretation changed a completed read outcome")
		}
		if changed && result.Verification.State != evidence.VerificationChanged {
			t.Fatal("completed reads concealed a changed target generation")
		}
		if !changed &&
			(result.Associations[0].State == evidence.AssociationConfirmed || result.Associations[0].State == evidence.AssociationForeign) {
			t.Fatal("completed reads concealed unconfirmed ownership")
		}
	}
}

func TestRelatedExcludesJobPodFromStatefulSetMembership(t *testing.T) {
	t.Parallel()
	client := testClient(t, func(writer http.ResponseWriter, incoming *http.Request) {
		if strings.HasSuffix(incoming.URL.Path, "/openbaoclusters/bao") {
			writeBody(t, writer, targetDocument("cluster-1", `,"generation":1`))
			return
		}
		parts := strings.Split(incoming.URL.Path, "/")
		resource := parts[len(parts)-1]
		page := relatedPage(resource)
		if resource == "pods" {
			page = strings.Replace(page, `"apiVersion":"apps/v1","kind":"StatefulSet"`, `"apiVersion":"batch/v1","kind":"Job"`, 1)
			page = strings.Replace(page, `"uid":"set-1"`, `"uid":"backup-job"`, 1)
		}
		writeBody(t, writer, page)
	})
	result, err := client.Related(context.Background(), selectedTarget(t), relatedPlan())
	if err != nil || result.Outcome != evidence.CollectionComplete || result.Pods.Collection.Items != 1 {
		t.Fatalf("Job Pod changed completed collection: %s %v", result.Outcome, err)
	}
	for _, association := range result.Associations {
		if association.Subject.Kind == "Pod" && association.State != evidence.AssociationForeign {
			t.Fatal("non-StatefulSet controller was not excluded from workload membership")
		}
	}
}
