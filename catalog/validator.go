package catalog

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	// MaxURLLength is the maximum allowed URL length in bytes.
	MaxURLLength = 2048
	// MaxURLsPerRequest is the maximum number of URLs accepted per array per request.
	MaxURLsPerRequest = 20
)

// validateURL checks that a URL is http/https, has a valid host, and is within
// the length limit. The full URL is intentionally NOT included in error messages
// to avoid leaking credentials that may appear in the URL (e.g. user:pass@host).
func validateURL(raw string) error {
	if len(raw) > MaxURLLength {
		return fmt.Errorf("URL exceeds maximum length of %d characters", MaxURLLength)
	}
	if raw == "" {
		return fmt.Errorf("URL must not be empty")
	}

	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return fmt.Errorf("URL is not valid: %v", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("URL scheme %q is not allowed; use http or https", scheme)
	}

	if parsed.Host == "" {
		return fmt.Errorf("URL is missing a host")
	}

	return nil
}

// validateURLSlice validates every URL in a slice and enforces the per-request limit.
func validateURLSlice(urls []string, field string) error {
	if len(urls) > MaxURLsPerRequest {
		return fmt.Errorf("%s: maximum %d URLs allowed per request, got %d",
			field, MaxURLsPerRequest, len(urls))
	}
	for i, u := range urls {
		if err := validateURL(u); err != nil {
			return fmt.Errorf("%s[%d]: %w", field, i, err)
		}
	}
	return nil
}

// validateNonEmpty checks that a string field is non-empty after trimming whitespace.
func validateNonEmpty(value, field string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required and must be non-empty", field)
	}
	return nil
}
