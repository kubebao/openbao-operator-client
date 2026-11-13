package adapter

// Contract selects approved fields and omission semantics. Selection is explicit;
// discovery and an API version alone do not establish operator compatibility.
type Contract string

const (
	// V1Alpha1Snapshot projects the inspected source contract, including maintenance and restore-retention evidence.
	V1Alpha1Snapshot Contract = "openbao.org/v1alpha1@9671c306/restore-retention-v1"
	// V1Alpha1Unqualified projects the same approved fields without inferring omitted values.
	V1Alpha1Unqualified Contract = "openbao.org/v1alpha1/unqualified-v4"
	// KubeBaoV1Alpha1Snapshot projects the inspected native API contract. Workload metadata retains openbao.org keys.
	KubeBaoV1Alpha1Snapshot Contract = "kubebao.org/v1alpha1@22894cdc/restore-retention-v1"
	// KubeBaoV1Alpha1Unqualified projects native API fields without inferring omitted values.
	KubeBaoV1Alpha1Unqualified Contract = "kubebao.org/v1alpha1/unqualified-v2"
	KubernetesAppsV1           Contract = "kubernetes/apps/v1@v0.37.2/workload-pools-v1"
	KubernetesCoreV1           Contract = "kubernetes/core/v1@v0.37.2/workload-pools-v1"
)

type Capability string

const (
	CapabilityCluster           Capability = "cluster"
	CapabilityConditions        Capability = "conditions"
	CapabilityVersions          Capability = "versions"
	CapabilityReadiness         Capability = "readiness"
	CapabilityBackup            Capability = "backup"
	CapabilityUpgrade           Capability = "upgrade"
	CapabilityRestoreTest       Capability = "restore_test"
	CapabilityRestore           Capability = "restore"
	CapabilityRestoreUIDBinding Capability = "restore_uid_binding"
	CapabilityRestrictions      Capability = "restrictions"
	CapabilityStatefulSet       Capability = "statefulset"
	CapabilityPod               Capability = "pod"
)

// ContractInfo describes library projection coverage. It is not API discovery or
// an operator integration qualification. Returned kind and capability slices are independent copies.
type ContractInfo struct {
	ID                Contract
	APIGroup          string
	APIVersion        string
	SourceRevision    string
	KubernetesVersion string
	Kinds             []string
	Capabilities      []Capability
}

func Describe(contract Contract) (ContractInfo, error) {
	info := ContractInfo{ID: contract, APIGroup: "openbao.org", APIVersion: "openbao.org/v1alpha1"}
	switch contract {
	case V1Alpha1Snapshot:
		info.SourceRevision = "9671c30623d609f948cf073e167f376de4ea9a85"
	case V1Alpha1Unqualified:
	case KubeBaoV1Alpha1Snapshot, KubeBaoV1Alpha1Unqualified:
		info.APIGroup, info.APIVersion = "kubebao.org", "kubebao.org/v1alpha1"
		if contract == KubeBaoV1Alpha1Snapshot {
			info.SourceRevision = "22894cdc0a4aa5c442a07f2bf9902fe28ce02ea1"
		}
	case KubernetesAppsV1:
		return ContractInfo{
			ID: contract, APIGroup: "apps", APIVersion: "apps/v1", KubernetesVersion: "v0.37.2",
			SourceRevision: "9671c30623d609f948cf073e167f376de4ea9a85",
			Kinds:          []string{"StatefulSet"}, Capabilities: []Capability{CapabilityStatefulSet, CapabilityReadiness},
		}, nil
	case KubernetesCoreV1:
		return ContractInfo{
			ID: contract, APIVersion: "v1", KubernetesVersion: "v0.37.2",
			SourceRevision: "9671c30623d609f948cf073e167f376de4ea9a85",
			Kinds:          []string{"Pod"}, Capabilities: []Capability{CapabilityPod, CapabilityConditions},
		}, nil
	default:
		return ContractInfo{}, &ProjectionError{Code: UnsupportedContract}
	}
	info.Capabilities = []Capability{
		CapabilityCluster, CapabilityConditions, CapabilityVersions, CapabilityReadiness,
		CapabilityBackup, CapabilityUpgrade, CapabilityRestoreTest, CapabilityRestore, CapabilityRestrictions,
	}
	info.Kinds = []string{"OpenBaoCluster", "OpenBaoRestore"}
	if inspectedOperatorContract(contract) {
		info.Capabilities = append(info.Capabilities, CapabilityRestoreUIDBinding)
	}
	return info, nil
}

func inspectedOperatorContract(contract Contract) bool {
	return contract == V1Alpha1Snapshot || contract == KubeBaoV1Alpha1Snapshot
}
