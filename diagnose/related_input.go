package diagnose

import (
	"fmt"

	"github.com/kubebao/openbao-operator-client/evidence"
)

func (c ClusterInput) validateRelatedInputs(p Policy) error {
	if c.StatefulSets != nil {
		if err := validateResourceInput(c, *c.StatefulSets, p, "apps", "v1", "statefulsets",
			func(v evidence.StatefulSet) (evidence.Reference, error) {
				return v.Metadata.Reference, v.Validate()
			}); err != nil {
			return evidence.At("statefulSets", err)
		}
	}
	if c.Pods != nil {
		if err := validateResourceInput(c, *c.Pods, p, "", "v1", "pods",
			func(v evidence.Pod) (evidence.Reference, error) {
				return v.Metadata.Reference, v.Validate()
			}); err != nil {
			return evidence.At("pods", err)
		}
	}
	if c.Restores != nil {
		err := validateResourceInput(c, *c.Restores, p, c.Subject.Group, c.Collection.Scope.Version, "openbaorestores",
			func(v evidence.Restore) (evidence.Reference, error) {
				return v.Reference, v.Validate()
			})
		return evidence.At("restores", err)
	}
	return nil
}

func validateResourceInput[T any](cluster ClusterInput, input ResourceInput[T], p Policy, group, version, resource string,
	describe func(T) (evidence.Reference, error),
) error {
	read := input.Collection
	if err := read.Validate(); err != nil {
		return invalidCapture("collection", err)
	}
	if err := validateResourceScope(cluster.Subject, read.Scope, group, version, resource); err != nil {
		return err
	}
	if read.Items < len(input.Observations) {
		return invalidCapture("collection.items", nil)
	}
	if read.Outcome == evidence.CollectionComplete {
		if input.Observations == nil {
			return invalidCapture("observations", nil)
		}
		if read.Items != len(input.Observations) {
			return invalidCapture("collection.items", nil)
		}
	}
	if err := validateResourceTiming(cluster, read, p); err != nil {
		return err
	}
	seenUID, seenName := map[string]bool{}, map[string]bool{}
	apiVersion := version
	if group != "" {
		apiVersion = group + "/" + version
	}
	for index, value := range input.Observations {
		path := fmt.Sprintf("observations[%d]", index)
		ref, err := describe(value)
		if err != nil {
			return invalidCapture(path, err)
		}
		if ref.APIVersion != apiVersion {
			return invalidCapture(path+".reference.apiVersion", nil)
		}
		id := ref.Identity
		if id.Origin != read.Scope.Origin || id.Namespace != read.Scope.Namespace || id.Group != group {
			return invalidCapture(path+".reference.identity", nil)
		}
		if seenUID[id.UID] || seenName[id.Name] {
			return invalidCapture(path+".reference.identity", nil)
		}
		if read.Outcome == evidence.CollectionComplete && !read.Interval.Contains(*ref.CollectedAt.Value) {
			return invalidCapture(path+".reference.collectedAt", nil)
		}
		seenUID[id.UID], seenName[id.Name] = true, true
	}
	return nil
}

func validateResourceScope(subject evidence.Identity, scope evidence.Scope, group, version, resource string) error {
	if scope.Origin != subject.Origin || scope.Namespace != subject.Namespace {
		return invalidCapture("collection.scope", nil)
	}
	if scope.Group != group {
		return invalidCapture("collection.scope.group", nil)
	}
	if scope.Version != version {
		return invalidCapture("collection.scope.version", nil)
	}
	if scope.Resource != resource {
		return invalidCapture("collection.scope.resource", nil)
	}
	if scope.Action != evidence.ReadList {
		return invalidCapture("collection.scope.action", nil)
	}
	return nil
}

func validateResourceTiming(cluster ClusterInput, read evidence.Collection, p Policy) error {
	if !read.Interval.CompletedAt.IsZero() && read.Interval.CompletedAt.After(p.ReferenceTime) {
		return invalidCapture("collection.interval.completedAt", nil)
	}
	if read.Interval.StartedAt.IsZero() {
		return nil
	}
	if cluster.Observation != nil && cluster.Observation.Reference.CollectedAt.Value.After(read.Interval.StartedAt) {
		return invalidCapture("collection.interval.startedAt", nil)
	}
	if cluster.Verification != nil && !cluster.Verification.Read.Interval.StartedAt.IsZero() &&
		read.Interval.CompletedAt.After(cluster.Verification.Read.Interval.StartedAt) {
		return invalidCapture("collection.interval.completedAt", nil)
	}
	return nil
}
