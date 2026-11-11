package evidence

import "strings"

type VerificationState string

const (
	VerificationVerified     VerificationState = "verified"
	VerificationChanged      VerificationState = "changed"
	VerificationInsufficient VerificationState = "insufficient"
	VerificationDeleted      VerificationState = "deleted"
	VerificationReplaced     VerificationState = "replaced"
	VerificationFailed       VerificationState = "failed"
)

// TargetVerification compares one selected cluster with a later named read.
// Verified means matching UID and positive metadata generation, not an atomic snapshot.
type TargetVerification struct {
	SchemaVersion    string            `json:"schemaVersion"`
	AdapterContract  string            `json:"adapterContract"`
	Before           Reference         `json:"before"`
	BeforeGeneration Integer           `json:"beforeGeneration"`
	After            *Reference        `json:"after,omitempty"`
	AfterGeneration  Integer           `json:"afterGeneration"`
	Read             Collection        `json:"read"`
	State            VerificationState `json:"state"`
}

func (v TargetVerification) Validate() error {
	if v.SchemaVersion != SchemaVersion || !validText(v.AdapterContract, 253, false) || v.Before.Validate() != nil ||
		v.Before.Identity.Kind != "OpenBaoCluster" || v.BeforeGeneration.Validate() != nil ||
		v.BeforeGeneration.State == ValueImplied || v.AfterGeneration.Validate() != nil || v.AfterGeneration.State == ValueImplied ||
		v.Read.Validate() != nil {
		return ErrInvalidEvidence
	}
	id, scope := v.Before.Identity, v.Read.Scope
	if scope.Action != ReadGet || scope.Resource != "openbaoclusters" || scope.Origin != id.Origin ||
		scope.Group != id.Group || scope.Namespace != id.Namespace || scope.Name != id.Name ||
		scope.Version != strings.TrimPrefix(v.Before.APIVersion, id.Group+"/") {
		return ErrInvalidEvidence
	}
	if v.Read.Outcome != CollectionNotCollected && v.Before.CollectedAt.Value.After(v.Read.Interval.StartedAt) {
		return ErrInvalidEvidence
	}
	if v.Read.Outcome != CollectionComplete {
		return v.validateIncomplete()
	}
	if v.After == nil || v.After.Validate() != nil || v.After.Identity != id || v.After.APIVersion != v.Before.APIVersion ||
		!v.Read.Interval.Contains(*v.After.CollectedAt.Value) || v.Read.Items != 1 {
		return ErrInvalidEvidence
	}
	positive := v.BeforeGeneration.State == ValueReported && v.AfterGeneration.State == ValueReported &&
		*v.BeforeGeneration.Value > 0 && *v.AfterGeneration.Value > 0
	expected := VerificationInsufficient
	if positive {
		expected = VerificationChanged
		if *v.BeforeGeneration.Value == *v.AfterGeneration.Value {
			expected = VerificationVerified
		}
	}
	if v.State == expected {
		return nil
	}
	return ErrInvalidEvidence
}

func (v TargetVerification) validateIncomplete() error {
	if v.After != nil || v.AfterGeneration.State != ValueUnknown || v.Read.Outcome == CollectionNotRequested {
		return ErrInvalidEvidence
	}
	expected := VerificationFailed
	switch v.Read.Outcome {
	case CollectionNotCollected:
		expected = VerificationInsufficient
	case CollectionDeleted:
		expected = VerificationDeleted
	case CollectionReplaced:
		expected = VerificationReplaced
	}
	if v.State != expected {
		return ErrInvalidEvidence
	}
	return nil
}
