package evidence

// GenerationState describes acknowledgement of one resource generation.
type GenerationState string

const (
	// GenerationUnknown means a positive resource or observed generation is absent.
	GenerationUnknown GenerationState = "unknown"
	// GenerationStale means the producer acknowledges an earlier generation.
	GenerationStale GenerationState = "stale"
	// GenerationCurrent means both positive generations are equal.
	GenerationCurrent GenerationState = "current"
	// GenerationInconsistent means a value is negative or acknowledges a future generation.
	GenerationInconsistent GenerationState = "inconsistent"
)

// CompareGeneration compares a reported generation with its acknowledgement.
// Call it separately for resource status and each individual condition.
// Equality establishes generation acknowledgement, not observation recency.
func CompareGeneration(generation, observed *int64) GenerationState {
	if (generation != nil && *generation < 0) || (observed != nil && *observed < 0) {
		return GenerationInconsistent
	}
	if generation == nil || observed == nil || *generation == 0 || *observed == 0 {
		return GenerationUnknown
	}
	switch {
	case *observed < *generation:
		return GenerationStale
	case *observed > *generation:
		return GenerationInconsistent
	default:
		return GenerationCurrent
	}
}
