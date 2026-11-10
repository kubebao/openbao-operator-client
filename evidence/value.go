package evidence

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ValueState separates field presence, validation, and documented omission semantics.
type ValueState string

const (
	ValueUnknown  ValueState = "unknown"
	ValueReported ValueState = "reported"
	ValueImplied  ValueState = "implied"
	ValueInvalid  ValueState = "invalid"
)

// ErrInvalidEvidence identifies invalid normalized values without exposing source data.
var ErrInvalidEvidence = errors.New("invalid normalized evidence")

// Integer preserves signed generations and exact integer values. Adapters enforce field-specific ranges.
// Only an implied zero is valid. Invalid and unknown fields retain no source value.
type Integer struct {
	State ValueState `json:"state"`
	Value *int64     `json:"value,omitempty"`
}

func (v Integer) Validate() error { return validateValue(v.State, v.Value, int64(0), true) }

// Boolean permits implied false only under an adapter's documented omission contract.
type Boolean struct {
	State ValueState `json:"state"`
	Value *bool      `json:"value,omitempty"`
}

func (v Boolean) Validate() error { return validateValue(v.State, v.Value, false, true) }

// Text contains an approved structured value, never a raw provider message.
type Text struct {
	State ValueState `json:"state"`
	Value *string    `json:"value,omitempty"`
}

func (v Text) Validate() error {
	if err := validateValue(v.State, v.Value, "", false); err != nil {
		return err
	}
	if v.Value != nil && !validText(*v.Value, 4096, false) {
		return ErrInvalidEvidence
	}
	return nil
}

// Timestamp records one producer or transition timestamp independently of collection time.
type Timestamp struct {
	State ValueState `json:"state"`
	Value *time.Time `json:"value,omitempty"`
}

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

func validText(value string, limit int, empty bool) bool {
	return (empty || value != "") && len(value) <= limit && utf8.ValidString(value) &&
		!strings.ContainsFunc(value, unicode.IsControl) && !strings.ContainsRune(value, unicode.ReplacementChar)
}
