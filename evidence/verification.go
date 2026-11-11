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

// TargetVerification compares one selected cluster with a later named GET or
// complete, unfiltered namespace LIST. A LIST preserves list-only reader permissions.
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
	if scope.Resource != "openbaoclusters" || scope.Origin != id.Origin ||
		scope.Group != id.Group || scope.Namespace != id.Namespace ||
		scope.Version != strings.TrimPrefix(v.Before.APIVersion, id.Group+"/") {
		return ErrInvalidEvidence
	}
	if scope.Action != ReadGet && scope.Action != ReadList || scope.Selector != "" ||
		scope.Action == ReadGet && scope.Name != id.Name {
		return ErrInvalidEvidence
	}
	if v.Read.Outcome != CollectionNotCollected && v.Before.CollectedAt.Value.After(v.Read.Interval.StartedAt) {
		return ErrInvalidEvidence
	}
	if v.Read.Outcome != CollectionComplete {
		return v.validateIncomplete()
	}
	if scope.Action == ReadList && v.After == nil {
		if v.State == VerificationDeleted && v.AfterGeneration.State == ValueUnknown {
			return nil
		}
		return ErrInvalidEvidence
	}
	if v.After == nil || v.After.Validate() != nil || v.After.APIVersion != v.Before.APIVersion ||
		!v.Read.Interval.Contains(*v.After.CollectedAt.Value) || v.Read.Items < 1 || scope.Action == ReadGet && v.Read.Items != 1 {
		return ErrInvalidEvidence
	}
	afterID := v.After.Identity
	afterID.UID = id.UID
	if afterID != id {
		return ErrInvalidEvidence
	}
	if v.After.Identity.UID != id.UID {
		if v.State == VerificationReplaced {
			return nil
		}
		return ErrInvalidEvidence
	}
	if v.State == generationVerification(v.BeforeGeneration, v.AfterGeneration) {
		return nil
	}
	return ErrInvalidEvidence
}

func generationVerification(before, after Integer) VerificationState {
	positive := before.State == ValueReported && after.State == ValueReported && *before.Value > 0 && *after.Value > 0
	expected := VerificationInsufficient
	if positive {
		expected = VerificationChanged
		if *before.Value == *after.Value {
			expected = VerificationVerified
		}
	}
	return expected
}

// NewTargetVerification checks one supplied read without performing I/O. A complete
// LIST must retain every candidate before filtering. Partial lists cannot verify a
// target or establish deletion, even when they contain a matching observation.
func NewTargetVerification(before Cluster, read Collection, observations []Cluster) (TargetVerification, error) {
	value := TargetVerification{
		SchemaVersion: SchemaVersion, AdapterContract: before.AdapterContract, Before: before.Reference,
		BeforeGeneration: before.Generation, AfterGeneration: Integer{State: ValueUnknown}, Read: read,
		State: VerificationFailed,
	}
	if before.Validate() != nil || read.Validate() != nil || read.Items < len(observations) {
		return TargetVerification{}, ErrInvalidEvidence
	}
	if read.Outcome == CollectionComplete {
		if observations == nil || read.Items != len(observations) {
			return TargetVerification{}, ErrInvalidEvidence
		}
		value.State = VerificationDeleted
	}
	seenUID, seenName := map[string]bool{}, map[string]bool{}
	for _, after := range observations {
		id := after.Reference.Identity
		if after.Validate() != nil || after.AdapterContract != before.AdapterContract ||
			after.Reference.APIVersion != before.Reference.APIVersion ||
			id.Origin != read.Scope.Origin || id.Namespace != read.Scope.Namespace ||
			seenUID[id.UID] ||
			seenName[id.Name] ||
			!read.Interval.Contains(*after.Reference.CollectedAt.Value) {
			return TargetVerification{}, ErrInvalidEvidence
		}
		seenUID[id.UID], seenName[id.Name] = true, true
		if read.Outcome != CollectionComplete || id.Name != before.Reference.Identity.Name {
			continue
		}
		ref := after.Reference
		value.After, value.AfterGeneration = &ref, after.Generation
		value.State = generationVerification(before.Generation, after.Generation)
		if id.UID != before.Reference.Identity.UID {
			value.State = VerificationReplaced
		}
	}
	switch read.Outcome {
	case CollectionNotCollected:
		value.State = VerificationInsufficient
	case CollectionDeleted:
		value.State = VerificationDeleted
	case CollectionReplaced:
		value.State = VerificationReplaced
	}
	if value.Validate() != nil {
		return TargetVerification{}, ErrInvalidEvidence
	}
	return value, nil
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
