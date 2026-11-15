package adapter

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/kubebao/openbao-operator-client/evidence"
)

// ErrorCode classifies a projection failure without retaining source data.
type ErrorCode string

const (
	// CodeInvalidInput means the input is malformed or violates the projection contract.
	CodeInvalidInput ErrorCode = "invalid_input"
	// CodeProjectionLimit means the input exceeds a projection limit.
	CodeProjectionLimit ErrorCode = "projection_limit"
	// CodeUnsupportedContract means the contract is unknown or does not cover the requested kind.
	CodeUnsupportedContract ErrorCode = "unsupported_contract"
	// CodeResourceMismatch means the document's API version, kind, or group version does not match the contract.
	CodeResourceMismatch ErrorCode = "resource_mismatch"
)

const (
	// defaultMaxBytes bounds one input document by default.
	defaultMaxBytes = 8 * 1024 * 1024
	// defaultMaxConditions bounds one retained condition set by default.
	defaultMaxConditions = 256
	// defaultMaxEntries bounds one projected list by default.
	defaultMaxEntries = 256
	// maxNestingDepth bounds JSON nesting during decoding.
	maxNestingDepth = 64
	// maxSnapshotKeyLength bounds an object-store snapshot key.
	maxSnapshotKeyLength = 1024
)

// ProjectionError exposes a bounded classification without a raw parser error or source value.
type ProjectionError struct{ Code ErrorCode }

// Error returns a fixed message for the error code.
func (e *ProjectionError) Error() string {
	switch e.Code {
	case CodeProjectionLimit:
		return "projection limit exceeded"
	case CodeUnsupportedContract:
		return "unsupported projection contract"
	case CodeResourceMismatch:
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

// DefaultProjectionLimits returns 8 MiB documents and 256 retained conditions and list entries.
func DefaultProjectionLimits() ProjectionLimits {
	return ProjectionLimits{
		MaxBytes:      defaultMaxBytes,
		MaxConditions: defaultMaxConditions,
		MaxEntries:    defaultMaxEntries,
	}
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)

// This is the public spec.version pattern from the pinned v1alpha1 API source.
var versionIdentifier = regexp.MustCompile(
	`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)` +
		`(-(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?` +
		`(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`,
)

// decodeObject decodes one bounded JSON object. It rejects duplicate object keys,
// invalid UTF-8, trailing data, and excessive nesting. Numbers never pass through
// floating point.
func decodeObject(raw []byte, limits ProjectionLimits) (map[string]any, error) {
	if limits.MaxBytes <= 0 || limits.MaxConditions <= 0 || limits.MaxEntries <= 0 {
		return nil, &ProjectionError{Code: CodeInvalidInput}
	}
	if len(raw) > limits.MaxBytes {
		return nil, &ProjectionError{Code: CodeProjectionLimit}
	}
	if !utf8.Valid(raw) {
		return nil, &ProjectionError{Code: CodeInvalidInput}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decodeValue(decoder, 0)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, &ProjectionError{Code: CodeInvalidInput}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, &ProjectionError{Code: CodeInvalidInput}
	}
	return object, nil
}

func decodeValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > maxNestingDepth {
		return nil, &ProjectionError{Code: CodeProjectionLimit}
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, &ProjectionError{Code: CodeInvalidInput}
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
			return nil, &ProjectionError{Code: CodeInvalidInput}
		}
		return items, nil
	default:
		return nil, &ProjectionError{Code: CodeInvalidInput}
	}
}

func decodeMembers(decoder *json.Decoder, depth int) (map[string]any, error) {
	object := map[string]any{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, &ProjectionError{Code: CodeInvalidInput}
		}
		key, ok := token.(string)
		if !ok {
			return nil, &ProjectionError{Code: CodeInvalidInput}
		}
		if _, duplicate := object[key]; duplicate {
			return nil, &ProjectionError{Code: CodeInvalidInput}
		}
		value, err := decodeValue(decoder, depth+1)
		if err != nil {
			return nil, err
		}
		object[key] = value
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
		return nil, &ProjectionError{Code: CodeInvalidInput}
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
	if !ok || !evidence.ValidText(parsed, evidence.MaxNameLength) {
		return evidence.Text{State: evidence.ValueInvalid}
	}
	if token && !identifier.MatchString(parsed) {
		return evidence.Text{State: evidence.ValueInvalid}
	}
	return evidence.Text{State: evidence.ValueReported, Value: &parsed}
}

var snapshotKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// snapshotKey projects a bounded object-store name, not an identifier or URL.
func snapshotKey(raw map[string]any, path ...string) evidence.Text {
	value, state := field(raw, path...)
	if state != evidence.ValueReported {
		return evidence.Text{State: state}
	}
	parsed, ok := value.(string)
	if !ok || len(parsed) > maxSnapshotKeyLength || !snapshotKeyPattern.MatchString(parsed) {
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
	if err != nil {
		return evidence.Timestamp{State: evidence.ValueInvalid}
	}
	if parsed.IsZero() || parsed.Year() < 1 || parsed.Year() > 9999 {
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
