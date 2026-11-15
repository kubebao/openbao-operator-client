package evidence

import (
	"errors"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MaxNameLength is the Kubernetes DNS subdomain limit. It bounds names, namespaces,
// UIDs, API groups, versions, kinds, and other identifiers in normalized evidence.
const MaxNameLength = 253

const (
	// maxTextLength bounds an approved structured text value.
	maxTextLength = 4096
	// maxSelectorLength bounds a declared label selector.
	maxSelectorLength = 4096
	// maxFieldLength bounds an approved field reference within a resource.
	maxFieldLength = 512
	// maxConditionTypeLength bounds a condition type.
	maxConditionTypeLength = 128
)

// ValueState separates field presence, validation, and documented omission semantics.
type ValueState string

const (
	// ValueUnknown means the field was not observed. It retains no value.
	ValueUnknown ValueState = "unknown"
	// ValueReported means the producer reported the retained value.
	ValueReported ValueState = "reported"
	// ValueImplied means an adapter derived the retained zero value from a documented omission rule.
	ValueImplied ValueState = "implied"
	// ValueInvalid means the field was present but failed validation. It retains no value.
	ValueInvalid ValueState = "invalid"
)

// ErrInvalidEvidence identifies invalid normalized values without exposing source data.
var ErrInvalidEvidence = errors.New("invalid normalized evidence")

// Integer preserves signed generations and exact integer values. Adapters enforce field-specific ranges.
// Only an implied zero is valid. Invalid and unknown fields retain no source value.
type Integer struct {
	State ValueState `json:"state"`
	Value *int64     `json:"value,omitempty"`
}

// Validate checks that the value is consistent with its state.
func (v Integer) Validate() error { return validateValue(v.State, v.Value, int64(0), true) }

// Boolean permits implied false only under an adapter's documented omission contract.
type Boolean struct {
	State ValueState `json:"state"`
	Value *bool      `json:"value,omitempty"`
}

// Validate checks that the value is consistent with its state.
func (v Boolean) Validate() error { return validateValue(v.State, v.Value, false, true) }

// Text contains an approved structured value, never a raw provider message.
type Text struct {
	State ValueState `json:"state"`
	Value *string    `json:"value,omitempty"`
}

// Validate checks that the value is consistent with its state and that a
// retained value is nonempty, bounded, approved text.
func (v Text) Validate() error {
	if err := validateValue(v.State, v.Value, "", false); err != nil {
		return err
	}
	if v.Value != nil && !ValidText(*v.Value, maxTextLength) {
		return ErrInvalidEvidence
	}
	return nil
}

// Timestamp records one producer or transition timestamp independently of collection time.
type Timestamp struct {
	State ValueState `json:"state"`
	Value *time.Time `json:"value,omitempty"`
}

// Validate checks that the value is consistent with its state and that a
// retained instant is nonzero and within years 1 through 9999.
func (v Timestamp) Validate() error {
	if err := validateValue(v.State, v.Value, time.Time{}, false); err != nil {
		return err
	}
	if v.Value != nil && !validInstant(*v.Value) {
		return ErrInvalidEvidence
	}
	return nil
}

func validInstant(value time.Time) bool {
	return !value.IsZero() && value.Year() >= 1 && value.Year() <= 9999
}

func validateValue[T comparable](state ValueState, value *T, zero T, implied bool) error {
	switch state {
	case ValueUnknown, ValueInvalid:
		if value == nil {
			return nil
		}
	case ValueReported:
		if value != nil {
			return nil
		}
	case ValueImplied:
		if implied && value != nil && *value == zero {
			return nil
		}
	}
	return ErrInvalidEvidence
}

// ValidText reports whether value is nonempty approved text: at most limit bytes
// of valid UTF-8 without control or replacement characters.
func ValidText(value string, limit int) bool {
	return value != "" &&
		len(value) <= limit &&
		utf8.ValidString(value) &&
		!strings.ContainsFunc(value, unicode.IsControl) &&
		!strings.ContainsRune(value, unicode.ReplacementChar)
}

// validOptionalText reports whether value is empty or approved text.
func validOptionalText(value string, limit int) bool {
	return value == "" || ValidText(value, limit)
}

// validateCount accepts a reported or unknown int32 count without an implied value.
func validateCount(v Integer) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if v.State == ValueImplied {
		return ErrInvalidEvidence
	}
	if v.Value != nil && (*v.Value < 0 || *v.Value > math.MaxInt32) {
		return ErrInvalidEvidence
	}
	return nil
}

// validateReadyCount accepts an int32 count, including an implied zero.
func validateReadyCount(v Integer) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if v.Value != nil && (*v.Value < 0 || *v.Value > math.MaxInt32) {
		return ErrInvalidEvidence
	}
	return nil
}

// validateEnum accepts an unknown or invalid value, or a reported value from values.
func validateEnum(value Text, values ...string) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if value.State == ValueImplied {
		return ErrInvalidEvidence
	}
	if value.Value != nil {
		for _, allowed := range values {
			if *value.Value == allowed {
				return nil
			}
		}
		return ErrInvalidEvidence
	}
	return nil
}
