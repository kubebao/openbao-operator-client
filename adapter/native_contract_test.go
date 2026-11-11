package adapter_test

import (
	"bytes"
	"reflect"
	"slices"
	"testing"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func nativeDocument(raw []byte) []byte {
	return bytes.ReplaceAll(raw, []byte("openbao.org/v1alpha1"), []byte("kubebao.org/v1alpha1"))
}

func nativeOptions() adapter.ProjectionOptions {
	options := projectionOptions()
	options.Contract = adapter.KubeBaoV1Alpha1Snapshot
	return options
}

func TestNativeContractDescription(t *testing.T) {
	t.Parallel()
	for _, contract := range []adapter.Contract{adapter.KubeBaoV1Alpha1Snapshot, adapter.KubeBaoV1Alpha1Unqualified} {
		info, err := adapter.Describe(contract)
		if err != nil || info.ID != contract || info.APIGroup != "kubebao.org" || info.APIVersion != "kubebao.org/v1alpha1" ||
			!reflect.DeepEqual(info.Kinds, []string{"OpenBaoCluster", "OpenBaoRestore"}) {
			t.Fatalf("native description = %+v, %v", info, err)
		}
		inspected := contract == adapter.KubeBaoV1Alpha1Snapshot
		if slices.Contains(info.Capabilities, adapter.CapabilityRestoreUIDBinding) != inspected ||
			(inspected && info.SourceRevision != "22894cdc0a4aa5c442a07f2bf9902fe28ce02ea1") ||
			(!inspected && info.SourceRevision != "") {
			t.Fatal("native qualification boundary changed")
		}
	}
}

func TestNativeProjectionPreservesApprovedFacts(t *testing.T) {
	t.Parallel()
	for _, raw := range [][]byte{clusterFixture(t), operationalFixture(t)} {
		legacy, err := adapter.ProjectCluster(raw, projectionOptions())
		if err != nil {
			t.Fatal(err)
		}
		native, err := adapter.ProjectCluster(nativeDocument(raw), nativeOptions())
		if err != nil {
			t.Fatal(err)
		}
		if native.Reference.Identity.Group != "kubebao.org" || native.Reference.APIVersion != "kubebao.org/v1alpha1" ||
			native.AdapterContract != string(adapter.KubeBaoV1Alpha1Snapshot) {
			t.Fatal("native API identity was lost")
		}
		native.AdapterContract, native.Reference.Identity.Group, native.Reference.APIVersion =
			legacy.AdapterContract, legacy.Reference.Identity.Group, legacy.Reference.APIVersion
		if !reflect.DeepEqual(native, legacy) {
			t.Fatal("native projection changed shared field or metadata semantics")
		}
		assertSafeReplay(t, native)
	}
	legacy, err := adapter.ProjectRestore(restoreFixture(t), projectionOptions())
	if err != nil {
		t.Fatal(err)
	}
	native, err := adapter.ProjectRestore(nativeDocument(restoreFixture(t)), nativeOptions())
	if err != nil {
		t.Fatal(err)
	}
	native.AdapterContract, native.Reference.Identity.Group, native.Reference.APIVersion =
		legacy.AdapterContract, legacy.Reference.Identity.Group, legacy.Reference.APIVersion
	if !reflect.DeepEqual(native, legacy) {
		t.Fatal("native restore projection changed shared field semantics")
	}
	assertSafeReplay(t, native)
}

func TestNativeContractDoesNotFallbackAcrossGroups(t *testing.T) {
	t.Parallel()
	_, err := adapter.ProjectCluster(clusterFixture(t), nativeOptions())
	assertProjectionError(t, err, adapter.ResourceMismatch)
	_, err = adapter.ProjectCluster(nativeDocument(clusterFixture(t)), projectionOptions())
	assertProjectionError(t, err, adapter.ResourceMismatch)
	_, err = adapter.ProjectRestore(restoreFixture(t), nativeOptions())
	assertProjectionError(t, err, adapter.ResourceMismatch)
	_, err = adapter.ProjectRestore(nativeDocument(restoreFixture(t)), projectionOptions())
	assertProjectionError(t, err, adapter.ResourceMismatch)

	options := nativeOptions()
	options.Contract = adapter.KubeBaoV1Alpha1Unqualified
	cluster, err := adapter.ProjectCluster(nativeDocument(clusterFixture(t)), options)
	if err != nil || cluster.Paused.State != evidence.ValueUnknown || cluster.ReadyReplicas.State != evidence.ValueUnknown {
		t.Fatalf("unqualified native omissions = %+v, %v", cluster, err)
	}
}

func TestNativeDiscoveryIsContractBound(t *testing.T) {
	t.Parallel()
	raw := nativeDocument(discoveryDocument(`{"name":"openbaoclusters","kind":"OpenBaoCluster",` +
		`"namespaced":true,"verbs":["get","list","patch"]}`))
	discovery, err := adapter.ProjectDiscovery(raw, "openbaoclusters", nativeOptions())
	if err != nil || !adapter.Advertises(discovery, evidence.VerbGet) || discovery.Scope.Group != "kubebao.org" {
		t.Fatalf("native discovery = %+v, %v", discovery, err)
	}
	_, err = adapter.ProjectDiscovery(raw, "openbaoclusters", projectionOptions())
	assertProjectionError(t, err, adapter.ResourceMismatch)
}

func TestNativeRestoreAssociationPreservesGroupBoundary(t *testing.T) {
	t.Parallel()
	target, err := adapter.ProjectCluster(nativeDocument(operationalFixture(t)), nativeOptions())
	if err != nil {
		t.Fatal(err)
	}
	request, err := adapter.ProjectRestore(nativeDocument(restoreFixture(t)), nativeOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*evidence.Cluster, *evidence.Restore)
		want   evidence.AssociationState
	}{
		{"native", func(*evidence.Cluster, *evidence.Restore) {}, evidence.AssociationConfirmed},
		{"legacy-restore", func(_ *evidence.Cluster, r *evidence.Restore) {
			r.Reference.Identity.Group, r.Reference.APIVersion = "openbao.org", "openbao.org/v1alpha1"
			r.AdapterContract = string(adapter.V1Alpha1Snapshot)
		}, evidence.AssociationForeign},
		{"unqualified", func(c *evidence.Cluster, r *evidence.Restore) {
			c.AdapterContract, r.AdapterContract = string(adapter.KubeBaoV1Alpha1Unqualified), string(adapter.KubeBaoV1Alpha1Unqualified)
		}, evidence.AssociationUnconfirmed},
		{"spoofed-contract", func(c *evidence.Cluster, r *evidence.Restore) {
			c.AdapterContract, r.AdapterContract = string(adapter.V1Alpha1Snapshot), string(adapter.V1Alpha1Snapshot)
		}, evidence.AssociationUnconfirmed},
		{"wrong-version", func(c *evidence.Cluster, r *evidence.Restore) {
			c.Reference.APIVersion, r.Reference.APIVersion = "kubebao.org/v2", "kubebao.org/v2"
		}, evidence.AssociationUnconfirmed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, r := target, request
			tc.change(&c, &r)
			association, associationErr := adapter.AssociateRestore(c, r)
			if associationErr != nil || association.State != tc.want {
				t.Fatalf("association = %+v, %v", association, associationErr)
			}
		})
	}
}

func TestNativeWorkloadOwnerUsesNativeGroupAndLegacyMetadata(t *testing.T) {
	t.Parallel()
	target := clusterIdentity()
	target.Group = "kubebao.org"
	set, err := adapter.ProjectStatefulSet(nativeDocument(workloadFixture(t, "statefulset")), workloadOptions(adapter.KubernetesAppsV1))
	if err != nil {
		t.Fatal(err)
	}
	association, err := adapter.AssociateStatefulSet(set, target)
	if err != nil || association.State != evidence.AssociationConfirmed || set.Metadata.ClusterOwnerUID.Value == nil ||
		*set.Metadata.ClusterOwnerUID.Value != target.UID || set.Metadata.Maintenance.Value == nil ||
		*set.Metadata.Maintenance.Value != "false" {
		t.Fatalf("native workload association = %+v, %v", association, err)
	}
	legacy := target
	legacy.Group = "openbao.org"
	association, err = adapter.AssociateStatefulSet(set, legacy)
	if err != nil || association.State != evidence.AssociationConflict {
		t.Fatalf("same UID across API groups confirmed ownership: %+v, %v", association, err)
	}
}
