package evidence

import (
	"errors"
	"regexp"
)

// ValidationError identifies a normalized field without retaining or rendering
// its value. Unwrap preserves the original sentinel classifications.
type ValidationError struct {
	Path  string
	cause error
}

func (e *ValidationError) Error() string { return "invalid field: " + e.Path }
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
	if len(path) > 2048 || !validationPath.MatchString(path) {
		path = "input"
	}
	return &ValidationError{Path: path, cause: cause}
}

type fieldCheck struct {
	path string
	err  error
}

func checkFields(fields ...fieldCheck) error {
	for _, field := range fields {
		if field.err != nil {
			return At(field.path, field.err)
		}
	}
	return nil
}
