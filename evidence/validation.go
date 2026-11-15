package evidence

import (
	"errors"
	"regexp"
	"strconv"
)

// maxValidationPathLength bounds a rendered validation path.
const maxValidationPathLength = 2048

// ValidationError identifies a normalized field without retaining or rendering
// its value. Unwrap preserves the original sentinel classifications.
type ValidationError struct {
	Path  string
	cause error
}

// Error returns the field path. It never renders the cause or the field value.
func (e *ValidationError) Error() string { return "invalid field: " + e.Path }

// Unwrap returns the cause so that errors.Is matches its sentinel classification.
func (e *ValidationError) Unwrap() error { return e.cause }

var validationPath = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*(\[[0-9]+\])?(\.[a-zA-Z][a-zA-Z0-9]*(\[[0-9]+\])?)*$`)

// At adds a library-owned field name or numeric index to a validation error.
// It never formats the cause. Invalid paths are replaced with "input".
func At(path string, cause error) error {
	if cause == nil {
		return nil
	}
	var nested *ValidationError
	if errors.As(cause, &nested) {
		path += "." + nested.Path
	}
	if len(path) > maxValidationPathLength || !validationPath.MatchString(path) {
		path = "input"
	}
	return &ValidationError{Path: path, cause: cause}
}

// itemPath returns the path of one element of a named list.
func itemPath(name string, index int) string {
	return name + "[" + strconv.Itoa(index) + "]"
}

type fieldCheck struct {
	path string
	err  error
}

// checkFields returns the first failed check, attributed to its path.
func checkFields(fields ...fieldCheck) error {
	for _, field := range fields {
		if field.err != nil {
			return At(field.path, field.err)
		}
	}
	return nil
}

// validateSet checks the presence contract shared by normalized lists. Unknown
// and invalid lists retain no values. A reported list retains a non-nil slice,
// which can be empty. It reports whether the caller must validate the values.
func validateSet[T any](state ValueState, values []T) (bool, error) {
	switch state {
	case ValueUnknown, ValueInvalid:
		if values != nil {
			return false, At("values", ErrInvalidEvidence)
		}
		return false, nil
	case ValueReported:
		if values == nil {
			return false, At("values", ErrInvalidEvidence)
		}
		return true, nil
	default:
		return false, At("state", ErrInvalidEvidence)
	}
}

type observedField interface {
	Validate() error
	fieldState() ValueState
}

func (v Integer) fieldState() ValueState    { return v.State }
func (v Boolean) fieldState() ValueState    { return v.State }
func (v Text) fieldState() ValueState       { return v.State }
func (v Timestamp) fieldState() ValueState  { return v.State }
func (v Conditions) fieldState() ValueState { return v.State }
func (v Ordinals) fieldState() ValueState   { return v.State }

type objectField struct {
	path  string
	value observedField
}

// validateObject checks an object's presence state and its child fields. The
// children of an object that was not reported must remain unknown.
func validateObject(statePath string, state ObjectState, fields ...objectField) error {
	if !state.Valid() {
		return At(statePath, ErrInvalidEvidence)
	}
	for _, field := range fields {
		if err := field.value.Validate(); err != nil {
			return At(field.path, err)
		}
		if state != ObjectReported && field.value.fieldState() != ValueUnknown {
			return At(field.path, ErrInvalidEvidence)
		}
	}
	return nil
}

// validateChild checks that a nested object is unknown unless its parent was reported.
func validateChild(parent ObjectState, childPath string, child ObjectState) error {
	if parent != ObjectReported && child != ObjectUnknown {
		return At(childPath, ErrInvalidEvidence)
	}
	return nil
}
