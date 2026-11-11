package diagnose

import (
	"slices"

	"github.com/kubebao/openbao-operator-client/evidence"
)

func (c ClusterInput) validateRelatedInputs(p Policy) error {
	if c.StatefulSets != nil {
		if err := validateResourceInput(c, *c.StatefulSets, p, "apps", "v1", "statefulsets",
			func(v evidence.StatefulSet) (evidence.Reference, string, error) {
				return v.Metadata.Reference, v.AdapterContract, v.Validate()
			}); err != nil {
			return err
		}
	}
	if c.Pods != nil {
		if err := validateResourceInput(c, *c.Pods, p, "", "v1", "pods",
			func(v evidence.Pod) (evidence.Reference, string, error) {
				return v.Metadata.Reference, v.AdapterContract, v.Validate()
			}); err != nil {
			return err
		}
	}
	if c.Restores != nil {
		if c.Restores.AdapterContract != c.AdapterContract {
			return ErrInvalidCapture
		}
		return validateResourceInput(c, *c.Restores, p, c.Subject.Group, c.Collection.Scope.Version, "openbaorestores",
			func(v evidence.Restore) (evidence.Reference, string, error) {
				return v.Reference, v.AdapterContract, v.Validate()
			})
	}
	return nil
}

func validateResourceInput[T any](cluster ClusterInput, input ResourceInput[T], p Policy, group, version, resource string,
	describe func(T) (evidence.Reference, string, error),
) error {
	if !slices.Contains(p.ApprovedContracts, input.AdapterContract) {
		return ErrUnsupportedVersion
	}
	read, subject := input.Collection, cluster.Subject
	scope := read.Scope
	if read.Validate() != nil || scope.Origin != subject.Origin || scope.Namespace != subject.Namespace ||
		scope.Group != group || scope.Version != version || scope.Resource != resource || scope.Action != evidence.ReadList ||
		read.Items < len(input.Observations) || read.Outcome == evidence.CollectionComplete &&
		(input.Observations == nil || read.Items != len(input.Observations)) ||
		!read.Interval.CompletedAt.IsZero() && read.Interval.CompletedAt.After(p.ReferenceTime) {
		return ErrInvalidCapture
	}
	if !read.Interval.StartedAt.IsZero() {
		if cluster.Observation != nil && cluster.Observation.Reference.CollectedAt.Value.After(read.Interval.StartedAt) ||
			cluster.Verification != nil && !cluster.Verification.Read.Interval.StartedAt.IsZero() &&
				read.Interval.CompletedAt.After(cluster.Verification.Read.Interval.StartedAt) {
			return ErrInvalidCapture
		}
	}
	seenUID, seenName := map[string]bool{}, map[string]bool{}
	apiVersion := version
	if group != "" {
		apiVersion = group + "/" + version
	}
	for _, value := range input.Observations {
		ref, contract, err := describe(value)
		id := ref.Identity
		if err != nil || contract != input.AdapterContract || ref.APIVersion != apiVersion ||
			id.Origin != scope.Origin || id.Namespace != scope.Namespace ||
			id.Group != group || seenUID[id.UID] || seenName[id.Name] ||
			read.Outcome == evidence.CollectionComplete && !read.Interval.Contains(*ref.CollectedAt.Value) {
			return ErrInvalidCapture
		}
		seenUID[id.UID], seenName[id.Name] = true, true
	}
	return nil
}
