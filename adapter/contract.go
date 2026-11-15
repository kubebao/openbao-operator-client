package adapter

// Contract selects approved fields and omission semantics. Selection is explicit;
// discovery and an API version alone do not establish operator compatibility.
type Contract string

const (
	// OpenBaoV1Alpha1 projects the inspected source contract, including maintenance and restore-retention evidence.
	OpenBaoV1Alpha1 Contract = "openbao.org/v1alpha1"
	// OpenBaoV1Alpha1Unqualified projects the same approved fields without inferring omitted values.
	OpenBaoV1Alpha1Unqualified Contract = "openbao.org/v1alpha1/unqualified"
	// KubeBaoV1Alpha1 projects the inspected native API contract. Workload metadata retains openbao.org keys.
	KubeBaoV1Alpha1 Contract = "kubebao.org/v1alpha1"
	// KubeBaoV1Alpha1Unqualified projects native API fields without inferring omitted values.
	KubeBaoV1Alpha1Unqualified Contract = "kubebao.org/v1alpha1/unqualified"
	// KubernetesAppsV1 projects StatefulSet fields from the pinned Kubernetes apps/v1 API.
	KubernetesAppsV1 Contract = "apps/v1"
	// KubernetesCoreV1 projects Pod fields from the pinned Kubernetes core v1 API.
	KubernetesCoreV1 Contract = "v1"
)

// Capability names a category of evidence that a contract projects.
type Capability string

const (
	// CapabilityCluster projects cluster identity, generation, and phase.
	CapabilityCluster Capability = "cluster"
	// CapabilityConditions projects resource conditions without messages.
	CapabilityConditions Capability = "conditions"
	// CapabilityVersions projects desired and observed versions.
	CapabilityVersions Capability = "versions"
	// CapabilityReadiness projects desired and ready replica counts.
	CapabilityReadiness Capability = "readiness"
	// CapabilityBackup projects backup configuration and status.
	CapabilityBackup Capability = "backup"
	// CapabilityUpgrade projects rolling and blue-green upgrade status.
	CapabilityUpgrade Capability = "upgrade"
	// CapabilityRestoreTest projects restore-test status.
	CapabilityRestoreTest Capability = "restore_test"
	// CapabilityRestore projects OpenBaoRestore request and execution facts.
	CapabilityRestore Capability = "restore"
	// CapabilityRestoreUIDBinding projects restore target and execution UIDs.
	CapabilityRestoreUIDBinding Capability = "restore_uid_binding"
	// CapabilityRestrictions projects pause, maintenance, operation lock, break-glass, and restore hold facts.
	CapabilityRestrictions Capability = "restrictions"
	// CapabilityStatefulSet projects StatefulSet facts.
	CapabilityStatefulSet Capability = "statefulset"
	// CapabilityPod projects Pod facts.
	CapabilityPod Capability = "pod"
)

const (
	// operatorSourceRevision pins the inspected openbao.org operator API source.
	operatorSourceRevision = "9671c30623d609f948cf073e167f376de4ea9a85"
	// nativeSourceRevision pins the inspected kubebao.org API source.
	nativeSourceRevision = "7b9bbf8b1a2aa3a62e2235607073df3e912643b5"
	// kubernetesAPIVersion pins the inspected Kubernetes API module.
	kubernetesAPIVersion = "v0.37.2"
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

// Describe returns the projection coverage of a known contract. Qualified
// contracts carry the pinned source revision; unqualified contracts carry none.
func Describe(contract Contract) (ContractInfo, error) {
	info := ContractInfo{ID: contract, APIGroup: "openbao.org", APIVersion: "openbao.org/v1alpha1"}
	switch contract {
	case OpenBaoV1Alpha1:
		info.SourceRevision = operatorSourceRevision
	case OpenBaoV1Alpha1Unqualified:
	case KubeBaoV1Alpha1, KubeBaoV1Alpha1Unqualified:
		info.APIGroup, info.APIVersion = "kubebao.org", "kubebao.org/v1alpha1"
		if contract == KubeBaoV1Alpha1 {
			info.SourceRevision = nativeSourceRevision
		}
	case KubernetesAppsV1:
		return ContractInfo{
			ID:                contract,
			APIGroup:          "apps",
			APIVersion:        "apps/v1",
			SourceRevision:    operatorSourceRevision,
			KubernetesVersion: kubernetesAPIVersion,
			Kinds:             []string{"StatefulSet"},
			Capabilities:      []Capability{CapabilityStatefulSet, CapabilityReadiness},
		}, nil
	case KubernetesCoreV1:
		return ContractInfo{
			ID:                contract,
			APIVersion:        "v1",
			SourceRevision:    operatorSourceRevision,
			KubernetesVersion: kubernetesAPIVersion,
			Kinds:             []string{"Pod"},
			Capabilities:      []Capability{CapabilityPod, CapabilityConditions},
		}, nil
	default:
		return ContractInfo{}, &ProjectionError{Code: CodeUnsupportedContract}
	}
	info.Capabilities = []Capability{
		CapabilityCluster,
		CapabilityConditions,
		CapabilityVersions,
		CapabilityReadiness,
		CapabilityBackup,
		CapabilityUpgrade,
		CapabilityRestoreTest,
		CapabilityRestore,
		CapabilityRestrictions,
	}
	info.Kinds = []string{"OpenBaoCluster", "OpenBaoRestore"}
	if inspectedOperatorContract(contract) {
		info.Capabilities = append(info.Capabilities, CapabilityRestoreUIDBinding)
	}
	return info, nil
}

// inspectedOperatorContract reports whether a contract applies pinned omission rules.
func inspectedOperatorContract(contract Contract) bool {
	return contract == OpenBaoV1Alpha1 || contract == KubeBaoV1Alpha1
}
