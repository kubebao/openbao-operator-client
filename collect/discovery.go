package collect

import (
	"context"
	"encoding/json"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

// Discover reads only the selected contract's group/version discovery endpoint.
// It neither selects a contract nor starts resource reads.
func (c *Client) Discover(ctx context.Context, request Request) (result Result[evidence.Discovery], err error) {
	kind, describeErr := adapter.ResourceKind(request.Contract, request.Scope.Resource)
	if ctx == nil {
		return result, evidence.At("context", ErrInvalidRequest)
	}
	if describeErr != nil {
		return result, evidence.At("contract", ErrInvalidRequest)
	}
	if request.Scope.Action != evidence.ReadDiscovery {
		return result, evidence.At("scope.action", ErrInvalidRequest)
	}
	if validationErr := c.validate(request, kind, request.Scope.Resource); validationErr != nil {
		return result, validationErr
	}
	ctx, cancel := context.WithTimeout(ctx, request.Limits.Timeout)
	defer cancel()
	result = Result[evidence.Discovery]{Observations: []evidence.Discovery{}, Collection: evidence.Collection{
		Scope: request.Scope, Interval: evidence.Interval{StartedAt: time.Now().UTC()}, Outcome: evidence.CollectionPartial,
	}}
	defer finishCollection(ctx, &result.Collection)
	if ctx.Err() != nil {
		result.Collection.Outcome = contextOutcome(ctx, nil)
		return result, nil
	}
	budget := min(request.Limits.MaxResponseBytes, request.Limits.MaxBytes, request.Projection.MaxBytes)
	result.Requests = 1
	raw, count, outcome := c.read(ctx, c.requestURL(request.Scope, 0, ""), evidence.ReadDiscovery, budget)
	result.Bytes, result.Collection.Outcome = count, outcome
	if ctx.Err() != nil {
		result.Collection.Outcome = contextOutcome(ctx, nil)
		return result, nil
	}
	if outcome != evidence.CollectionComplete {
		return result, nil
	}
	value, projectErr := adapter.ProjectDiscovery(raw, request.Scope.Resource, adapter.ProjectionOptions{
		Origin: request.Scope.Origin, Contract: request.Contract, CollectedAt: time.Now().UTC(), Limits: request.Projection,
	})
	if projectErr != nil {
		result.Collection.Outcome = projectionOutcome(projectErr)
		return result, nil
	}
	encoded, encodeErr := json.Marshal(value)
	if encodeErr != nil {
		result.Collection.Outcome = evidence.CollectionInvalid
		return result, nil
	}
	if len(encoded) > request.Limits.MaxRetainedBytes {
		result.Collection.Outcome = evidence.CollectionSizeLimit
		return result, nil
	}
	result.Observations, result.RetainedBytes = []evidence.Discovery{value}, len(encoded)
	if value.State == evidence.ObjectReported {
		result.Collection.Items = 1
	}
	return result, nil
}
