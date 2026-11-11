package collect

import (
	"context"
	"time"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

type VerificationResult struct {
	Verification  evidence.TargetVerification `json:"verification"`
	Requests      int                         `json:"requests"`
	Bytes         int                         `json:"bytes"`
	RetainedBytes int                         `json:"retainedBytes"`
}

func (c *Client) validateTarget(target evidence.Cluster, request Request) error {
	if target.Validate() != nil || request.Contract != adapter.Contract(target.AdapterContract) ||
		target.Reference.CollectedAt.Value.After(time.Now()) || request.Scope.Action != evidence.ReadGet ||
		c.validate(request, "OpenBaoCluster", "openbaoclusters") != nil {
		return ErrInvalidRequest
	}
	id, scope := target.Reference.Identity, request.Scope
	apiVersion := scope.Version
	if scope.Group != "" {
		apiVersion = scope.Group + "/" + scope.Version
	}
	if scope.Origin != id.Origin || scope.Group != id.Group || scope.Namespace != id.Namespace ||
		scope.Name != id.Name || request.ExpectedUID != id.UID || target.Reference.APIVersion != apiVersion {
		return ErrInvalidRequest
	}
	return nil
}

func uncollectedVerification(target evidence.Cluster, request Request) evidence.TargetVerification {
	return evidence.TargetVerification{
		SchemaVersion: evidence.SchemaVersion, AdapterContract: target.AdapterContract,
		Before: target.Reference, BeforeGeneration: target.Generation, AfterGeneration: evidence.Integer{State: evidence.ValueUnknown},
		Read: evidence.Collection{Scope: request.Scope, Outcome: evidence.CollectionNotCollected}, State: evidence.VerificationInsufficient,
	}
}

// VerifyTarget performs exactly one pinned named GET. It never rediscovers a group.
func (c *Client) VerifyTarget(ctx context.Context, target evidence.Cluster, request Request) (VerificationResult, error) {
	if ctx == nil || c.validateTarget(target, request) != nil {
		return VerificationResult{}, ErrInvalidRequest
	}
	read, err := c.Clusters(ctx, request)
	if err != nil {
		return VerificationResult{}, err
	}
	value := uncollectedVerification(target, request)
	value.Read = read.Collection
	switch read.Collection.Outcome {
	case evidence.CollectionComplete:
		after := read.Observations[0]
		value.After, value.AfterGeneration = &after.Reference, after.Generation
		value.State = evidence.VerificationInsufficient
		if target.Generation.State == evidence.ValueReported && after.Generation.State == evidence.ValueReported &&
			*target.Generation.Value > 0 && *after.Generation.Value > 0 {
			value.State = evidence.VerificationChanged
			if *target.Generation.Value == *after.Generation.Value {
				value.State = evidence.VerificationVerified
			}
		}
	case evidence.CollectionDeleted:
		value.State = evidence.VerificationDeleted
	case evidence.CollectionReplaced:
		value.State = evidence.VerificationReplaced
	default:
		value.State = evidence.VerificationFailed
	}
	return VerificationResult{
		Verification: value, Requests: read.Requests, Bytes: read.Bytes, RetainedBytes: read.RetainedBytes,
	}, nil
}
