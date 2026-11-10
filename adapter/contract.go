package adapter

// Contract selects approved fields and omission semantics. Selection is explicit;
// discovery and an API version alone do not establish operator compatibility.
type Contract string

const (
	// V1Alpha1Snapshot implements the source contract inspected for milestone 1.
	V1Alpha1Snapshot Contract = "openbao.org/v1alpha1@9671c306"
	// V1Alpha1Unqualified projects the same approved fields without inferring omitted values.
	V1Alpha1Unqualified Contract = "openbao.org/v1alpha1/unqualified-v1"
)

type Capability string

const (
	CapabilityCluster    Capability = "cluster"
	CapabilityConditions Capability = "conditions"
	CapabilityVersions   Capability = "versions"
	CapabilityReadiness  Capability = "readiness"
)

// ContractInfo describes library projection coverage. It is not API discovery or
// an operator integration qualification. Returned capability slices are independent copies.
type ContractInfo struct {
	ID             Contract
	APIGroup       string
	APIVersion     string
	SourceRevision string
	Capabilities   []Capability
}

func Describe(contract Contract) (ContractInfo, error) {
	info := ContractInfo{ID: contract, APIGroup: "openbao.org", APIVersion: "openbao.org/v1alpha1"}
	switch contract {
	case V1Alpha1Snapshot:
		info.SourceRevision = "9671c30623d609f948cf073e167f376de4ea9a85"
	case V1Alpha1Unqualified:
	default:
		return ContractInfo{}, &ProjectionError{Code: UnsupportedContract}
	}
	info.Capabilities = []Capability{CapabilityCluster, CapabilityConditions, CapabilityVersions, CapabilityReadiness}
	return info, nil
}
