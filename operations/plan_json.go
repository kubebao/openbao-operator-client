package operations

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"unicode/utf8"
)

const maxPlanJSONBytes = 16 * 1024

// UnmarshalJSON rejects missing or null intent, unknown fields, and duplicate
// keys. A missing enabled field must never become a resume or disable request.
// Invalid input leaves the destination unchanged and returns no source text.
func (p *Plan) UnmarshalJSON(raw []byte) error {
	if p == nil || len(raw) > maxPlanJSONBytes || !utf8.Valid(raw) {
		return ErrInvalidPlan
	}
	tokens := json.NewDecoder(bytes.NewReader(raw))
	tokens.UseNumber()
	if consumePlanJSON(tokens, 0, "") != nil {
		return ErrInvalidPlan
	}
	if _, err := tokens.Token(); err != io.EOF {
		return ErrInvalidPlan
	}
	type plainPlan Plan
	var input struct {
		plainPlan
		Enabled *bool `json:"enabled"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || input.Enabled == nil {
		return ErrInvalidPlan
	}
	value := Plan(input.plainPlan)
	value.Enabled = *input.Enabled
	if value.Validate() != nil {
		return ErrInvalidPlan
	}
	*p = value
	return nil
}

var planJSONFields = map[string][]string{
	"":                       {"contract", "target", "generation", "control", "enabled", "maintenanceState"},
	"target":                 {"identity", "apiVersion", "resourceVersion", "collectedAt", "field"},
	"target.identity":        {"origin", "group", "kind", "namespace", "name", "uid"},
	"generation":             {"state", "value"},
	"target.resourceVersion": {"state", "value"},
	"target.collectedAt":     {"state", "value"},
}

func consumePlanJSON(decoder *json.Decoder, depth int, path string) error {
	if depth > 32 {
		return ErrInvalidPlan
	}
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalidPlan
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	if delimiter != '{' {
		return ErrInvalidPlan
	}
	keys := map[string]bool{}
	for decoder.More() {
		key, keyErr := decoder.Token()
		name, valid := key.(string)
		if keyErr != nil || !valid || keys[name] || !slices.Contains(planJSONFields[path], name) {
			return ErrInvalidPlan
		}
		keys[name] = true
		child := name
		if path != "" {
			child = path + "." + name
		}
		if consumePlanJSON(decoder, depth+1, child) != nil {
			return ErrInvalidPlan
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return ErrInvalidPlan
	}
	return nil
}
