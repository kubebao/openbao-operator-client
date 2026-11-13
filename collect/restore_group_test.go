package collect_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/collect"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func restoreGroupTarget(t *testing.T, contract adapter.Contract, group string) (evidence.Cluster, string) {
	t.Helper()
	raw := fmt.Sprintf(`{"apiVersion":%q,"kind":"OpenBaoCluster","metadata":{`+
		`"namespace":"team","name":"bao","uid":"cluster-1","generation":1,"resourceVersion":"10",`+
		`"annotations":{"openbao.org/restore-origin":"request-origin"}}}`, group+"/v1alpha1")
	target, err := adapter.ProjectCluster([]byte(raw), adapter.ProjectionOptions{
		Origin: "origin-1", Contract: contract, CollectedAt: time.Now().UTC().Add(-time.Second),
		Limits: adapter.DefaultProjectionLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return target, raw
}

func restoreGroupPlan(contract adapter.Contract, group string) collect.RelatedPlan {
	plan := relatedPlan()
	plan.StatefulSets, plan.Pods = nil, nil
	plan.Restores.Contract, plan.Restores.Scope.Group, plan.Restores.Scope.Selector = contract, group, ""
	plan.Verify.Contract, plan.Verify.Scope.Group = contract, group
	return plan
}

func assertNonemptyRestoreAssociations(t *testing.T, target evidence.Cluster, associations []evidence.Association) {
	t.Helper()
	want := map[string]evidence.AssociationState{
		"request-execution": evidence.AssociationConfirmed,
		"request-origin":    evidence.AssociationConfirmed,
		"request-candidate": evidence.AssociationCandidate,
		"request-foreign":   evidence.AssociationForeign,
		"request-conflict":  evidence.AssociationConflict,
		"request-migrated":  evidence.AssociationCandidate,
	}
	for _, association := range associations {
		if association.Validate() != nil || association.State != want[association.Subject.UID] ||
			association.Subject.Group != target.Reference.Identity.Group || association.Target != target.Reference.Identity {
			t.Fatal("nonempty restore collection changed UID association or API identity")
		}
	}
}

func TestNonemptyRestoreCollectionKeepsUIDAssociationsAndDeclaredGroup(t *testing.T) {
	t.Parallel()
	for _, contract := range []adapter.Contract{adapter.KubeBaoV1Alpha1, adapter.OpenBaoV1Alpha1} {
		t.Run(string(contract), func(t *testing.T) {
			info, err := adapter.Describe(contract)
			if err != nil {
				t.Fatal(err)
			}
			target, rawTarget := restoreGroupTarget(t, contract, info.APIGroup)
			prefix := "/prefix/apis/" + info.APIGroup + "/v1alpha1/namespaces/team/"
			paths := []string{}
			client := testClient(t, func(writer http.ResponseWriter, incoming *http.Request) {
				paths = append(paths, incoming.URL.Path)
				if incoming.Method != http.MethodGet {
					t.Error("restore observation issued a non-GET request")
				}
				switch incoming.URL.Path {
				case prefix + "openbaorestores":
					if incoming.URL.Query().Get("labelSelector") != "" || incoming.URL.Query().Get("limit") != "100" {
						t.Error("restore collection restricted or expanded the declared LIST")
					}
					writeBody(t, writer, fmt.Sprintf(`{"apiVersion":%q,"kind":"OpenBaoRestoreList",`+
						`"metadata":{"resourceVersion":"20"},"items":[`+
						`{"metadata":{"namespace":"team","name":"execution","uid":"request-execution"},`+
						`"spec":{"cluster":"bao","source":{"key":"SOURCE_CANARY"}},`+
						`"status":{"phase":"Running","execution":{"targetUID":"cluster-1",`+
						`"operationID":"operation-1","stage":"Created","jobName":"JOB_CANARY"},"message":"MESSAGE_CANARY"}},`+
						`{"metadata":{"namespace":"team","name":"origin","uid":"request-origin"},"spec":{"cluster":"bao"}},`+
						`{"metadata":{"namespace":"team","name":"candidate","uid":"request-candidate"},"spec":{"cluster":"bao"}},`+
						`{"metadata":{"namespace":"team","name":"foreign","uid":"request-foreign"},"spec":{"cluster":"bao"},`+
						`"status":{"target":{"uid":"replacement"}}},`+
						`{"metadata":{"namespace":"team","name":"conflict","uid":"request-conflict"},"spec":{"cluster":"bao"},`+
						`"status":{"target":{"uid":"cluster-1"},"execution":{"targetUID":"replacement"}}},`+
						`{"metadata":{"namespace":"team","name":"migrated","uid":"request-migrated",`+
						`"annotations":{"migration.kubebao.org/source-uid":"request-origin"}},"spec":{"cluster":"bao"}}]}`,
						info.APIVersion))
				case prefix + "openbaoclusters/bao":
					writeBody(t, writer, rawTarget)
				default:
					t.Error("restore collection left the declared API group or resource scope")
					writer.WriteHeader(http.StatusNotFound)
				}
			})
			result, err := client.Related(context.Background(), target, restoreGroupPlan(contract, info.APIGroup))
			if err != nil || result.Outcome != evidence.CollectionComplete || result.Verification.State != evidence.VerificationVerified ||
				result.Restores == nil || len(result.Restores.Observations) != 6 || len(result.Associations) != 6 {
				t.Fatalf("nonempty restore collection: outcome=%s error=%v", result.Outcome, err)
			}
			if !reflect.DeepEqual(paths, []string{prefix + "openbaorestores", prefix + "openbaoclusters/bao"}) {
				t.Fatalf("unexpected restore read sequence: %v", paths)
			}
			assertNonemptyRestoreAssociations(t, target, result.Associations)
			encoded, err := json.Marshal(result)
			if err != nil || strings.Contains(string(encoded), "CANARY") || strings.Contains(string(encoded), "migration.kubebao.org") {
				t.Fatal("restore source text or migration markers entered normalized evidence")
			}
		})
	}
}

func TestNonemptyRestoreCollectionRejectsAnotherGroupWithoutFallback(t *testing.T) {
	t.Parallel()
	for _, contract := range []adapter.Contract{adapter.KubeBaoV1Alpha1, adapter.OpenBaoV1Alpha1} {
		t.Run(string(contract), func(t *testing.T) {
			info, err := adapter.Describe(contract)
			if err != nil {
				t.Fatal(err)
			}
			target, rawTarget := restoreGroupTarget(t, contract, info.APIGroup)
			otherGroup := "kubebao.org"
			if info.APIGroup == otherGroup {
				otherGroup = "openbao.org"
			}
			prefix := "/prefix/apis/" + info.APIGroup + "/v1alpha1/namespaces/team/"
			calls := 0
			client := testClient(t, func(writer http.ResponseWriter, incoming *http.Request) {
				calls++
				switch incoming.URL.Path {
				case prefix + "openbaorestores":
					writeBody(t, writer, fmt.Sprintf(`{"apiVersion":%q,"kind":"OpenBaoRestoreList",`+
						`"metadata":{"resourceVersion":"20"},"items":[{"metadata":{"namespace":"team",`+
						`"name":"foreign","uid":"request-1"},"spec":{"cluster":"bao"},`+
						`"status":{"target":{"uid":"cluster-1"}}}]}`, otherGroup+"/v1alpha1"))
				case prefix + "openbaoclusters/bao":
					writeBody(t, writer, rawTarget)
				default:
					t.Error("invalid restore response triggered cross-group fallback")
					writer.WriteHeader(http.StatusNotFound)
				}
			})
			result, err := client.Related(context.Background(), target, restoreGroupPlan(contract, info.APIGroup))
			if err != nil || result.Outcome != evidence.CollectionPartial || calls != 2 || result.Restores == nil ||
				result.Restores.Collection.Outcome != evidence.CollectionInvalid || len(result.Restores.Observations) != 0 ||
				len(result.Associations) != 0 || result.Verification.State != evidence.VerificationVerified {
				t.Fatal("another group's restore response became admitted evidence or prevented independent target verification")
			}
		})
	}
}
