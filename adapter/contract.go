package adapter

// Contract selects approved fields and omission semantics. Selection is explicit;
// discovery and an API version alone do not establish operator compatibility.
type Contract string

const (
	// V1Alpha1Snapshot implements the source contract inspected for milestone 1.
	V1Alpha1Snapshot Contract = "openbao.org/v1alpha1@9671c306"
	// V1Alpha1Unqualified projects the same approved fields without inferring omitted values.
	V1Alpha1Unqualified Contract = "openbao.org/v1alpha1/unqualified-v1"
	KubernetesAppsV1    Contract = "kubernetes/apps/v1@v0.37.2"
	KubernetesCoreV1    Contract = "kubernetes/core/v1@v0.37.2"
)

type Capability string

const (
	CapabilityCluster      Capability = "cluster"
	CapabilityConditions   Capability = "conditions"
	CapabilityVersions     Capability = "versions"
	CapabilityReadiness    Capability = "readiness"
	CapabilityBackup       Capability = "backup"
	CapabilityUpgrade      Capability = "upgrade"
	CapabilityRestoreTest  Capability = "restore_test"
	CapabilityRestore      Capability = "restore"
	CapabilityRestrictions Capability = "restrictions"
	CapabilityStatefulSet  Capability = "statefulset"
	CapabilityPod          Capability = "pod"
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
	case KubernetesAppsV1:
		return ContractInfo{
			ID: contract, APIGroup: "apps", APIVersion: "apps/v1", KubernetesVersion: "v0.37.2",
			Kinds: []string{"StatefulSet"}, Capabilities: []Capability{CapabilityStatefulSet, CapabilityReadiness},
		}, nil
	case KubernetesCoreV1:
		return ContractInfo{
			ID: contract, APIVersion: "v1", KubernetesVersion: "v0.37.2",
			Kinds: []string{"Pod"}, Capabilities: []Capability{CapabilityPod, CapabilityConditions},
		}, nil
	default:
		return ContractInfo{}, &ProjectionError{Code: UnsupportedContract}
	}
	info.Capabilities = []Capability{
		CapabilityCluster, CapabilityConditions, CapabilityVersions, CapabilityReadiness,
		CapabilityBackup, CapabilityUpgrade, CapabilityRestoreTest, CapabilityRestore, CapabilityRestrictions,
	}
	info.Kinds = []string{"OpenBaoCluster", "OpenBaoRestore"}
	return info, nil
}
