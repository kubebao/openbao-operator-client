package apiendpoint_test

import (
	"errors"
	"testing"

	"github.com/kubebao/openbao-operator-client/internal/apiendpoint"
)

func TestParse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		raw   string
		valid bool
	}{
		{"https", "https://api.example:6443", true},
		{"http-prefix", "http://127.0.0.1:8001/prefix", true},
		{"trailing-slash", "https://api.example/prefix/", true},
		{"scheme", "ftp://api.example", false},
		{"no-host", "https:///prefix", false},
		{"port-only", "https://:6443", false},
		{"opaque", "https:api.example", false},
		{"user", "https://user@api.example", false},
		{"query", "https://api.example?x=1", false},
		{"force-query", "https://api.example?", false},
		{"fragment", "https://api.example#x", false},
		{"escaped", "https://api.example/a%2Fb", false},
		{"percent", "https://api.example/%25", false},
		{"backslash", `https://api.example/a\b`, false},
		{"dot", "https://api.example/./x", false},
		{"dot-dot", "https://api.example/x/..", false},
		{"unparsable", "https://api.example:port", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := apiendpoint.Parse(tc.raw)
			if (err == nil) != tc.valid {
				t.Fatalf("Parse(%q) error = %v, valid = %t", tc.raw, err, tc.valid)
			}
			if err != nil && !errors.Is(err, apiendpoint.ErrInvalid) {
				t.Fatalf("Parse(%q) error = %v, want ErrInvalid", tc.raw, err)
			}
		})
	}
}
