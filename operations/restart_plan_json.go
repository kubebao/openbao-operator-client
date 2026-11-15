package operations

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

var restartPlanJSONFields = map[string][]string{
	"":                       {"contract", "target", "generation", "runtimeState", "restartAt"},
	"target":                 planJSONFields["target"],
	"target.identity":        planJSONFields["target.identity"],
	"generation":             planJSONFields["generation"],
	"target.resourceVersion": planJSONFields["target.resourceVersion"],
	"target.collectedAt":     planJSONFields["target.collectedAt"],
}

// UnmarshalJSON rejects unknown fields and duplicate keys, then validates the
// decoded plan. Invalid input leaves the destination unchanged and returns no
// source text. A decoded plan that fails validation returns the validation field path.
func (p *RestartPlan) UnmarshalJSON(raw []byte) error {
	if p == nil || len(raw) > maxPlanJSONBytes || !utf8.Valid(raw) {
		return ErrInvalidPlan
	}
	tokens := json.NewDecoder(bytes.NewReader(raw))
	tokens.UseNumber()
	if consumePlanJSON(tokens, 0, "", restartPlanJSONFields) != nil {
		return ErrInvalidPlan
	}
	if _, err := tokens.Token(); err != io.EOF {
		return ErrInvalidPlan
	}
	type plain RestartPlan
	var input plain
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		return ErrInvalidPlan
	}
	if err := RestartPlan(input).Validate(); err != nil {
		return err
	}
	*p = RestartPlan(input)
	return nil
}
