package adapter

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/kubebao/openbao-operator-client/evidence"
)

type ErrorCode string

const (
	InvalidInput        ErrorCode = "invalid_input"
	ProjectionLimit     ErrorCode = "projection_limit"
	UnsupportedContract ErrorCode = "unsupported_contract"
	ResourceMismatch    ErrorCode = "resource_mismatch"
)

// ProjectionError exposes a bounded classification without a raw parser error or source value.
type ProjectionError struct{ Code ErrorCode }

func (e *ProjectionError) Error() string {
	switch e.Code {
	case ProjectionLimit:
		return "projection limit exceeded"
	case UnsupportedContract:
		return "unsupported projection contract"
	case ResourceMismatch:
		return "resource does not match projection contract"
	default:
		return "invalid projection input"
	}
}

// ProjectionLimits bound one input document and retained condition set.
// Zero or negative limits are rejected rather than interpreted as unbounded.
type ProjectionLimits struct {
	MaxBytes      int
	MaxConditions int
	MaxEntries    int
}

func DefaultProjectionLimits() ProjectionLimits {
	return ProjectionLimits{MaxBytes: 1024 * 1024, MaxConditions: 256, MaxEntries: 256}
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)

// This is the public spec.version pattern from the pinned v1alpha1 API source.
var versionIdentifier = regexp.MustCompile(
	`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)` +
		`(-(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?` +
		`(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`,
)

// decodeValue rejects duplicate object keys, invalid UTF-8, trailing data, and
// excessive nesting. Numbers never pass through floating point.
func decodeObject(raw []byte, limits ProjectionLimits) (map[string]any, error) {
	if limits.MaxBytes <= 0 || limits.MaxConditions <= 0 || limits.MaxEntries <= 0 {
		return nil, &ProjectionError{Code: InvalidInput}
	}
	if len(raw) > limits.MaxBytes {
		return nil, &ProjectionError{Code: ProjectionLimit}
	}
	if !utf8.Valid(raw) {
		return nil, &ProjectionError{Code: InvalidInput}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decodeValue(decoder, 0)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, &ProjectionError{Code: InvalidInput}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, &ProjectionError{Code: InvalidInput}
	}
	return object, nil
}

func decodeValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, &ProjectionError{Code: ProjectionLimit}
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, &ProjectionError{Code: InvalidInput}
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return token, nil
	}
	switch delimiter {
	case '{':
		return decodeMembers(decoder, depth)
	case '[':
		items := []any{}
		for decoder.More() {
			value, err := decodeValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			items = append(items, value)
		}
		if end, err := decoder.Token(); err != nil || end != json.Delim(']') {
			return nil, &ProjectionError{Code: InvalidInput}
		}
		return items, nil
	default:
		return nil, &ProjectionError{Code: InvalidInput}
	}
}

func decodeMembers(decoder *json.Decoder, depth int) (map[string]any, error) {
	object := map[string]any{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, &ProjectionError{Code: InvalidInput}
		}
		key, ok := token.(string)
		if !ok {
			return nil, &ProjectionError{Code: InvalidInput}
		}
		if _, duplicate := object[key]; duplicate {
			return nil, &ProjectionError{Code: InvalidInput}
		}
		value, err := decodeValue(decoder, depth+1)
		if err != nil {
			return nil, err
		}
		object[key] = value
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
		return nil, &ProjectionError{Code: InvalidInput}
	}
	return object, nil
}

func field(raw map[string]any, path ...string) (any, evidence.ValueState) {
	var value any = raw
	for _, part := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, evidence.ValueInvalid
		}
		value, ok = object[part]
		if !ok {
			return nil, evidence.ValueUnknown
		}
	}
	return value, evidence.ValueReported
}

func integer(raw map[string]any, path ...string) evidence.Integer {
	value, state := field(raw, path...)
	if state != evidence.ValueReported {
		return evidence.Integer{State: state}
	}
	number, ok := value.(json.Number)
	if !ok {
		return evidence.Integer{State: evidence.ValueInvalid}
	}
	parsed, err := number.Int64()
	if err != nil {
		return evidence.Integer{State: evidence.ValueInvalid}
	}
	return evidence.Integer{State: evidence.ValueReported, Value: &parsed}
}

func count(raw map[string]any, path ...string) evidence.Integer {
	result := integer(raw, path...)
	if result.Value != nil && (*result.Value < 0 || *result.Value > math.MaxInt32) {
		return evidence.Integer{State: evidence.ValueInvalid}
	}
	return result
}

func boolean(raw map[string]any, path ...string) evidence.Boolean {
	value, state := field(raw, path...)
	if state != evidence.ValueReported {
		return evidence.Boolean{State: state}
	}
	parsed, ok := value.(bool)
	if !ok {
		return evidence.Boolean{State: evidence.ValueInvalid}
	}
	return evidence.Boolean{State: evidence.ValueReported, Value: &parsed}
}

func text(raw map[string]any, token bool, path ...string) evidence.Text {
	value, state := field(raw, path...)
	if state != evidence.ValueReported {
		return evidence.Text{State: state}
	}
	parsed, ok := value.(string)
	if !ok || parsed == "" || len(parsed) > 253 || strings.ContainsFunc(parsed, unicode.IsControl) ||
		strings.ContainsRune(parsed, unicode.ReplacementChar) ||
		(token && !identifier.MatchString(parsed)) {
		return evidence.Text{State: evidence.ValueInvalid}
	}
	return evidence.Text{State: evidence.ValueReported, Value: &parsed}
}

func timestamp(raw map[string]any, path ...string) evidence.Timestamp {
	value, state := field(raw, path...)
	if state != evidence.ValueReported {
		return evidence.Timestamp{State: state}
	}
	text, ok := value.(string)
	if !ok {
		return evidence.Timestamp{State: evidence.ValueInvalid}
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil || parsed.IsZero() || parsed.Year() < 1 || parsed.Year() > 9999 {
		return evidence.Timestamp{State: evidence.ValueInvalid}
	}
	parsed = parsed.UTC()
	return evidence.Timestamp{State: evidence.ValueReported, Value: &parsed}
}

func version(raw map[string]any, path ...string) evidence.Text {
	value := text(raw, false, path...)
	if value.Value != nil && !versionIdentifier.MatchString(*value.Value) {
		return evidence.Text{State: evidence.ValueInvalid}
	}
	return value
}

func required(raw map[string]any, path ...string) string {
	value := text(raw, false, path...)
	if value.Value == nil {
		return ""
	}
	return *value.Value
}
