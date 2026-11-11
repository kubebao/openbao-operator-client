package evidence

import (
	"fmt"
	"strings"
)

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
	if err := v.validateHeader(); err != nil {
		return err
	}
	if err := v.validateScope(); err != nil {
		return err
	}
	if v.Read.Outcome != CollectionNotCollected && v.Before.CollectedAt.Value.After(v.Read.Interval.StartedAt) {
		return At("read.interval.startedAt", ErrInvalidEvidence)
	}
	if v.Read.Outcome != CollectionComplete {
		return v.validateIncomplete()
	}
	if v.Read.Scope.Action == ReadList && v.After == nil {
		if v.State == VerificationDeleted && v.AfterGeneration.State == ValueUnknown {
			return nil
		}
		return At("after", ErrInvalidEvidence)
	}
	return v.validateAfter()
}

func (v TargetVerification) validateHeader() error {
	if v.SchemaVersion != SchemaVersion {
		return At("schemaVersion", ErrInvalidEvidence)
	}
	if !validText(v.AdapterContract, 253, false) {
		return At("adapterContract", ErrInvalidEvidence)
	}
	if err := checkFields(fieldCheck{"before", v.Before.Validate()}, fieldCheck{"beforeGeneration", v.BeforeGeneration.Validate()},
		fieldCheck{"afterGeneration", v.AfterGeneration.Validate()}, fieldCheck{"read", v.Read.Validate()}); err != nil {
		return err
	}
	if v.Before.Identity.Kind != "OpenBaoCluster" {
		return At("before.identity.kind", ErrInvalidEvidence)
	}
	if v.BeforeGeneration.State == ValueImplied {
		return At("beforeGeneration", ErrInvalidEvidence)
	}
	if v.AfterGeneration.State == ValueImplied {
		return At("afterGeneration", ErrInvalidEvidence)
	}
	return nil
}

func (v TargetVerification) validateScope() error {
	id, scope := v.Before.Identity, v.Read.Scope
	if scope.Resource != "openbaoclusters" {
		return At("read.scope.resource", ErrInvalidEvidence)
	}
	if scope.Origin != id.Origin || scope.Group != id.Group || scope.Namespace != id.Namespace {
		return At("read.scope", ErrInvalidEvidence)
	}
	if scope.Version != strings.TrimPrefix(v.Before.APIVersion, id.Group+"/") {
		return At("read.scope.version", ErrInvalidEvidence)
	}
	if scope.Action != ReadGet && scope.Action != ReadList {
		return At("read.scope.action", ErrInvalidEvidence)
	}
	if scope.Selector != "" {
		return At("read.scope.selector", ErrInvalidEvidence)
	}
	if scope.Action == ReadGet && scope.Name != id.Name {
		return At("read.scope.name", ErrInvalidEvidence)
	}
	return nil
}

func (v TargetVerification) validateAfter() error {
	if v.After == nil {
		return At("after", ErrInvalidEvidence)
	}
	if err := At("after", v.After.Validate()); err != nil {
		return err
	}
	if v.After.APIVersion != v.Before.APIVersion {
		return At("after.apiVersion", ErrInvalidEvidence)
	}
	if !v.Read.Interval.Contains(*v.After.CollectedAt.Value) {
		return At("after.collectedAt", ErrInvalidEvidence)
	}
	if v.Read.Items < 1 || v.Read.Scope.Action == ReadGet && v.Read.Items != 1 {
		return At("read.items", ErrInvalidEvidence)
	}
	afterID, beforeID := v.After.Identity, v.Before.Identity
	afterID.UID = beforeID.UID
	if afterID != beforeID {
		return At("after.identity", ErrInvalidEvidence)
	}
	if v.After.Identity.UID != beforeID.UID {
		if v.State == VerificationReplaced {
			return nil
		}
		return At("state", ErrInvalidEvidence)
	}
	if v.State != generationVerification(v.BeforeGeneration, v.AfterGeneration) {
		return At("state", ErrInvalidEvidence)
	}
	return nil
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
	if err := before.Validate(); err != nil {
		return TargetVerification{}, At("before", err)
	}
	if err := read.Validate(); err != nil {
		return TargetVerification{}, At("read", err)
	}
	if read.Items < len(observations) {
		return TargetVerification{}, At("read.items", ErrInvalidEvidence)
	}
	if read.Outcome == CollectionComplete {
		if observations == nil || read.Items != len(observations) {
			return TargetVerification{}, At("observations", ErrInvalidEvidence)
		}
		value.State = VerificationDeleted
	}
	seenUID, seenName := map[string]bool{}, map[string]bool{}
	for index, after := range observations {
		id := after.Reference.Identity
		if err := after.Validate(); err != nil {
			return TargetVerification{}, At(fmt.Sprintf("observations[%d]", index), err)
		}
		path := fmt.Sprintf("observations[%d]", index)
		if after.AdapterContract != before.AdapterContract {
			return TargetVerification{}, At(path+".adapterContract", ErrInvalidEvidence)
		}
		if after.Reference.APIVersion != before.Reference.APIVersion {
			return TargetVerification{}, At(path+".reference.apiVersion", ErrInvalidEvidence)
		}
		if id.Origin != read.Scope.Origin || id.Namespace != read.Scope.Namespace {
			return TargetVerification{}, At(path+".reference.identity", ErrInvalidEvidence)
		}
		if seenUID[id.UID] || seenName[id.Name] {
			return TargetVerification{}, At(path+".reference.identity", ErrInvalidEvidence)
		}
		if !read.Interval.Contains(*after.Reference.CollectedAt.Value) {
			return TargetVerification{}, At(path+".reference.collectedAt", ErrInvalidEvidence)
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
	if err := value.Validate(); err != nil {
		return TargetVerification{}, err
	}
	return value, nil
}

func (v TargetVerification) validateIncomplete() error {
	if v.After != nil {
		return At("after", ErrInvalidEvidence)
	}
	if v.AfterGeneration.State != ValueUnknown {
		return At("afterGeneration", ErrInvalidEvidence)
	}
	if v.Read.Outcome == CollectionNotRequested {
		return At("read.outcome", ErrInvalidEvidence)
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
		return At("state", ErrInvalidEvidence)
	}
	return nil
}
