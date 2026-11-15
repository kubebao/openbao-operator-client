// Package apiendpoint validates caller-supplied Kubernetes API server base URLs.
package apiendpoint

import (
	"errors"
	"net/url"
	"strings"
)

// ErrInvalid reports a base URL that cannot be joined safely with API paths.
var ErrInvalid = errors.New("invalid API endpoint")

// Parse returns a base URL that is safe to join with API paths. It accepts an http
// or https URL with a host and an optional unescaped path prefix. It rejects user
// information, queries, fragments, opaque URLs, escaped paths, and dot segments.
func Parse(raw string) (url.URL, error) {
	base, err := url.Parse(raw)
	if err != nil {
		return url.URL{}, ErrInvalid
	}
	if base.Scheme != "https" && base.Scheme != "http" {
		return url.URL{}, ErrInvalid
	}
	if base.Hostname() == "" || base.Opaque != "" || base.User != nil {
		return url.URL{}, ErrInvalid
	}
	if base.RawQuery != "" || base.ForceQuery || base.Fragment != "" {
		return url.URL{}, ErrInvalid
	}
	if base.RawPath != "" || strings.ContainsAny(base.Path, `\%`) {
		return url.URL{}, ErrInvalid
	}
	for part := range strings.SplitSeq(base.Path, "/") {
		if part == "." || part == ".." {
			return url.URL{}, ErrInvalid
		}
	}
	return *base, nil
}
