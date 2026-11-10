package contracttest

import (
	"slices"
	"testing"
)

type scope struct {
	Selected   bool   `json:"selected"`
	Requested  bool   `json:"requested"`
	Applicable bool   `json:"applicable"`
	Outcome    string `json:"outcome"`
	Valid      bool   `json:"valid"`
	Count      int    `json:"count"`
}

type coverage struct {
	State  string `json:"state"`
	Absent bool   `json:"absent"`
}

func cover(s scope) coverage {
	switch {
	case !s.Selected:
		return coverage{State: "excluded"}
	case !s.Requested:
		return coverage{State: "not_requested"}
	case s.Outcome != "complete" || !s.Valid:
		return coverage{State: "insufficient"}
	case !s.Applicable:
		return coverage{State: "not_applicable"}
	default:
		return coverage{State: "evaluated", Absent: s.Count == 0}
	}
}

func TestCoverage(t *testing.T) {
	t.Parallel()
	cases := fixtures[[]struct {
		Name  string   `json:"name"`
		Scope scope    `json:"scope"`
		Want  coverage `json:"want"`
	}](t, "coverage")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) { equal(t, cover(tc.Scope), tc.Want) })
	}
}

type page struct {
	UIDs    []string `json:"uids"`
	Next    string   `json:"next"`
	Outcome string   `json:"outcome"`
	Elapsed int      `json:"elapsed"`
	Bytes   int      `json:"bytes"`
}

type limits struct {
	Pages      int `json:"pages"`
	Candidates int `json:"candidates"`
	Bytes      int `json:"bytes"`
	Duration   int `json:"duration"`
}

type collectionResult struct {
	Outcome string   `json:"outcome"`
	UIDs    []string `json:"uids"`
	Reads   int      `json:"reads"`
}

// A bounded transcript models list-page admission and final target identity verification.
func collect(pages []page, limit limits, before, after string) collectionResult {
	result := collectionResult{Outcome: "partial", UIDs: []string{}}
	if before == "" || limit.Pages <= 0 || limit.Candidates <= 0 || limit.Bytes <= 0 || limit.Duration <= 0 {
		result.Outcome = "invalid"
		return result
	}
	tokens := map[string]bool{}
	bytes := 0
	elapsed := 0
	for _, p := range pages {
		if result.Reads == limit.Pages {
			result.Outcome = "page_limit"
			return result
		}
		result.Reads++
		if p.Elapsed < elapsed || p.Bytes < 0 ||
			!slices.Contains([]string{"complete", "denied", "unsupported", "cancelled", "timeout"}, p.Outcome) {
			result.Outcome = "invalid"
			return result
		}
		elapsed = p.Elapsed
		if p.Outcome != "complete" {
			result.Outcome = p.Outcome
			return result
		}
		if p.Elapsed >= limit.Duration {
			result.Outcome = "timeout"
			return result
		}
		if p.Bytes > limit.Bytes-bytes || len(p.UIDs) > limit.Candidates-len(result.UIDs) {
			result.Outcome = "size_limit"
			return result
		}
		bytes += p.Bytes
		for _, uid := range p.UIDs {
			if uid == "" || slices.Contains(result.UIDs, uid) {
				result.Outcome = "invalid"
				return result
			}
			result.UIDs = append(result.UIDs, uid)
		}
		if p.Next == "" {
			result.Outcome = "complete"
			break
		}
		if tokens[p.Next] {
			result.Outcome = "pagination_loop"
			return result
		}
		tokens[p.Next] = true
	}
	if after == "" {
		result.Outcome = "deleted"
	} else if before != after {
		result.Outcome = "replaced"
	}
	slices.Sort(result.UIDs)
	return result
}

func TestCollectionLimits(t *testing.T) {
	t.Parallel()
	cases := fixtures[[]struct {
		Name   string           `json:"name"`
		Pages  []page           `json:"pages"`
		Limits limits           `json:"limits"`
		Before string           `json:"before"`
		After  string           `json:"after"`
		Want   collectionResult `json:"want"`
	}](t, "collection")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) { equal(t, collect(tc.Pages, tc.Limits, tc.Before, tc.After), tc.Want) })
	}
}
