package operations

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

// UnmarshalJSON validates the current restart plan without retaining source errors.
func (p *RestartPlan) UnmarshalJSON(raw []byte) error {
	if p == nil || len(raw) > maxPlanJSONBytes || !utf8.Valid(raw) {
		return ErrInvalidPlan
	}
	fields := map[string][]string{
		"":       {"contract", "target", "generation", "runtimeState", "restartAt"},
		"target": planJSONFields["target"], "target.identity": planJSONFields["target.identity"],
		"generation": planJSONFields["generation"], "target.resourceVersion": planJSONFields["target.resourceVersion"],
		"target.collectedAt": planJSONFields["target.collectedAt"],
	}
	tokens := json.NewDecoder(bytes.NewReader(raw))
	tokens.UseNumber()
	if consumePlanJSON(tokens, 0, "", fields) != nil {
		return ErrInvalidPlan
	}
	if _, err := tokens.Token(); err != io.EOF {
		return ErrInvalidPlan
	}
	type plain RestartPlan
	var input plain
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || RestartPlan(input).Validate() != nil {
		return ErrInvalidPlan
	}
	*p = RestartPlan(input)
	return nil
}
