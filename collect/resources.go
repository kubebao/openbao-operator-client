package collect

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

type projection[T any] struct {
	kind, resource string
	one            func([]byte, adapter.ProjectionOptions) (T, error)
	list           func([]byte, adapter.ProjectionOptions) (adapter.Page[T], error)
	reference      func(T) evidence.Reference
}

// Clusters reads only the requested GET or LIST scope. It performs no discovery.
func (c *Client) Clusters(ctx context.Context, request Request) (Result[evidence.Cluster], error) {
	return resources(ctx, c, request, projection[evidence.Cluster]{
		kind: "OpenBaoCluster", resource: "openbaoclusters", one: adapter.ProjectCluster, list: adapter.ProjectClusterList,
		reference: func(value evidence.Cluster) evidence.Reference { return value.Reference },
	})
}

func (c *Client) Restores(ctx context.Context, request Request) (Result[evidence.Restore], error) {
	return resources(ctx, c, request, projection[evidence.Restore]{
		kind: "OpenBaoRestore", resource: "openbaorestores", one: adapter.ProjectRestore, list: adapter.ProjectRestoreList,
		reference: func(value evidence.Restore) evidence.Reference { return value.Reference },
	})
}

func (c *Client) StatefulSets(ctx context.Context, request Request) (Result[evidence.StatefulSet], error) {
	return resources(ctx, c, request, projection[evidence.StatefulSet]{
		kind: "StatefulSet", resource: "statefulsets", one: adapter.ProjectStatefulSet, list: adapter.ProjectStatefulSetList,
		reference: func(value evidence.StatefulSet) evidence.Reference { return value.Metadata.Reference },
	})
}

func (c *Client) Pods(ctx context.Context, request Request) (Result[evidence.Pod], error) {
	return resources(ctx, c, request, projection[evidence.Pod]{
		kind: "Pod", resource: "pods", one: adapter.ProjectPod, list: adapter.ProjectPodList,
		reference: func(value evidence.Pod) evidence.Reference { return value.Metadata.Reference },
	})
}

func projectionOutcome(err error) evidence.CollectionOutcome {
	var failure *adapter.ProjectionError
	if errors.As(err, &failure) && failure.Code == adapter.ProjectionLimit {
		return evidence.CollectionSizeLimit
	}
	return evidence.CollectionInvalid
}

func resources[T any](ctx context.Context, client *Client, request Request, project projection[T]) (result Result[T], err error) {
	if ctx == nil {
		return result, evidence.At("context", ErrInvalidRequest)
	}
	if request.Scope.Action != evidence.ReadGet && request.Scope.Action != evidence.ReadList {
		return result, evidence.At("scope.action", ErrInvalidRequest)
	}
	if validationErr := client.validate(request, project.kind, project.resource); validationErr != nil {
		return result, validationErr
	}
	started := time.Now().UTC()
	ctx, cancel := context.WithTimeout(ctx, request.Limits.Timeout)
	defer cancel()
	result = Result[T]{
		Observations: []T{},
		Collection: evidence.Collection{
			Scope: request.Scope, Interval: evidence.Interval{StartedAt: started}, Outcome: evidence.CollectionPartial,
		},
	}
	defer finish(ctx, &result, project)
	seen, tokens := map[string]bool{}, map[string]bool{}
	version, next := "", ""
	for result.Requests < request.Limits.MaxPages {
		if ctx.Err() != nil {
			result.Collection.Outcome = contextOutcome(ctx, nil)
			return result, nil
		}
		budget := min(request.Limits.MaxResponseBytes, request.Projection.MaxBytes, request.Limits.MaxBytes-result.Bytes)
		if budget <= 0 {
			result.Collection.Outcome = evidence.CollectionSizeLimit
			return result, nil
		}
		result.Requests++
		raw, readBytes, outcome := client.read(
			ctx,
			client.requestURL(request.Scope, request.Limits.PageSize, next),
			request.Scope.Action,
			budget,
		)
		result.Bytes += readBytes
		result.Collection.Outcome = outcome
		if ctx.Err() != nil {
			result.Collection.Outcome = contextOutcome(ctx, nil)
			return result, nil
		}
		if outcome != evidence.CollectionComplete {
			return result, nil
		}
		page, readErr := projectPage(raw, request, project, time.Now().UTC())
		if readErr != nil {
			result.Collection.Outcome = projectionOutcome(readErr)
			return result, nil
		}
		if request.Scope.Action == evidence.ReadList && version != "" && page.ResourceVersion != version {
			result.Collection.Outcome = evidence.CollectionInvalid
			return result, nil
		}
		version = page.ResourceVersion
		size, outcome := admit(page.Observations, request, project, seen, result.Collection.Items, result.RetainedBytes)
		result.Collection.Outcome = outcome
		if outcome != evidence.CollectionComplete {
			return result, nil
		}
		result.Collection.Items += len(page.Observations)
		if request.ExpectedUID != "" && project.reference(page.Observations[0]).Identity.UID != request.ExpectedUID {
			result.Collection.Outcome = evidence.CollectionReplaced
			return result, nil
		}
		result.Observations = append(result.Observations, page.Observations...)
		result.RetainedBytes += size
		if page.Continue == "" {
			return result, nil
		}
		if tokens[page.Continue] {
			result.Collection.Outcome = evidence.CollectionPaginationLoop
			return result, nil
		}
		tokens[page.Continue], next = true, page.Continue
	}
	result.Collection.Outcome = evidence.CollectionPageLimit
	return result, nil
}

func finish[T any](ctx context.Context, result *Result[T], project projection[T]) {
	slices.SortFunc(result.Observations, func(a, b T) int {
		return slices.Compare(identityParts(project.reference(a).Identity), identityParts(project.reference(b).Identity))
	})
	finishCollection(ctx, &result.Collection)
}

func finishCollection(ctx context.Context, collection *evidence.Collection) {
	completed := time.Now()
	if collection.Outcome == evidence.CollectionComplete {
		deadline, _ := ctx.Deadline()
		if ctx.Err() != nil || !completed.Before(deadline) {
			collection.Outcome = contextOutcome(ctx, context.DeadlineExceeded)
		}
	}
	collection.Interval.CompletedAt = completed.UTC()
}

func projectPage[T any](raw []byte, request Request, project projection[T], collected time.Time) (adapter.Page[T], error) {
	options := adapter.ProjectionOptions{
		Origin: request.Scope.Origin, Contract: request.Contract, CollectedAt: collected, Limits: request.Projection,
	}
	if request.Scope.Action == evidence.ReadList {
		return project.list(raw, options)
	}
	value, err := project.one(raw, options)
	if err != nil {
		return adapter.Page[T]{}, err
	}
	return adapter.Page[T]{Observations: []T{value}}, nil
}

// Pages are admitted atomically. A bad page cannot discard earlier admitted facts.
func admit[T any](values []T, request Request, project projection[T], seen map[string]bool, count, retained int,
) (int, evidence.CollectionOutcome) {
	if len(values) > request.Limits.MaxCandidates-count {
		return 0, evidence.CollectionSizeLimit
	}
	pageUIDs := map[string]bool{}
	size := 0
	for _, value := range values {
		ref := project.reference(value)
		id, scope := ref.Identity, request.Scope
		if id.Origin != scope.Origin || id.Group != scope.Group || id.Kind != project.kind ||
			scope.Namespace != "" && id.Namespace != scope.Namespace || scope.Name != "" && id.Name != scope.Name ||
			seen[id.UID] || pageUIDs[id.UID] {
			return 0, evidence.CollectionInvalid
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return 0, evidence.CollectionInvalid
		}
		if len(encoded) > request.Limits.MaxRetainedBytes-retained-size {
			return 0, evidence.CollectionSizeLimit
		}
		size += len(encoded)
		pageUIDs[id.UID] = true
	}
	for uid := range pageUIDs {
		seen[uid] = true
	}
	return size, evidence.CollectionComplete
}

func identityParts(id evidence.Identity) []string {
	return []string{id.Origin, id.Group, id.Kind, id.Namespace, id.Name, id.UID}
}
