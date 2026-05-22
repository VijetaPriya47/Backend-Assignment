package catalog

import (
	"strings"
	"testing"
)

func TestValidateURL_Valid(t *testing.T) {
	cases := []string{
		"https://cdn.example.com/img.jpg",
		"http://cdn.example.com/img.jpg",
		"https://cdn.example.com/path/to/file.mp4",
		"http://localhost:8080/resource",
	}
	for _, u := range cases {
		if err := validateURL(u); err != nil {
			t.Errorf("expected %q to be valid, got: %v", u, err)
		}
	}
}

func TestValidateURL_Invalid(t *testing.T) {
	cases := []struct {
		url    string
		reason string
	}{
		{"not-a-url", "no scheme"},
		{"ftp://cdn.example.com/img.jpg", "wrong scheme"},
		{"//cdn.example.com/img.jpg", "no scheme"},
		{strings.Repeat("a", MaxURLLength+1), "too long"},
		{"https://", "missing host"},
		{"", "empty string"},
	}
	for _, c := range cases {
		if err := validateURL(c.url); err == nil {
			t.Errorf("expected %q (%s) to fail, but it passed", c.url, c.reason)
		}
	}
}

func TestValidateURL_CredentialsNotLeakedInError(t *testing.T) {
	sensitiveURL := "https://user:s3cr3t@cdn.example.com/img.jpg"
	// A valid URL with credentials should pass (we don't strip creds, we just
	// don't echo them back in errors).
	// An invalid one (bad scheme) must not include the password in the error.
	badScheme := "ftp://user:s3cr3t@cdn.example.com/img.jpg"
	err := validateURL(badScheme)
	if err == nil {
		t.Fatal("expected ftp:// to fail")
	}
	if strings.Contains(err.Error(), "s3cr3t") {
		t.Errorf("error message must not contain URL credentials, got: %v", err)
	}
	_ = sensitiveURL // valid https with creds passes fine
}

func TestValidateURLSlice_TooMany(t *testing.T) {
	urls := make([]string, MaxURLsPerRequest+1)
	for i := range urls {
		urls[i] = "https://cdn.example.com/img.jpg"
	}
	if err := validateURLSlice(urls, "image_urls"); err == nil {
		t.Errorf("expected error for %d URLs, got nil", len(urls))
	}
}

func TestValidateURLSlice_ExactlyAtLimit(t *testing.T) {
	urls := make([]string, MaxURLsPerRequest)
	for i := range urls {
		urls[i] = "https://cdn.example.com/img.jpg"
	}
	if err := validateURLSlice(urls, "image_urls"); err != nil {
		t.Errorf("exactly %d URLs should be valid, got: %v", MaxURLsPerRequest, err)
	}
}

func TestValidateURLSlice_Empty(t *testing.T) {
	if err := validateURLSlice([]string{}, "image_urls"); err != nil {
		t.Errorf("empty slice should be valid, got: %v", err)
	}
}

func TestValidateURLSlice_Nil(t *testing.T) {
	if err := validateURLSlice(nil, "image_urls"); err != nil {
		t.Errorf("nil slice should be valid, got: %v", err)
	}
}

func TestValidateURLSlice_ErrorIncludesIndex(t *testing.T) {
	urls := []string{
		"https://cdn.example.com/good.jpg",
		"not-a-url",
	}
	err := validateURLSlice(urls, "image_urls")
	if err == nil {
		t.Fatal("expected error for invalid URL in slice")
	}
	if !strings.Contains(err.Error(), "[1]") {
		t.Errorf("error should reference index 1, got: %v", err)
	}
}

func TestValidateNonEmpty_Valid(t *testing.T) {
	if err := validateNonEmpty("Widget A", "name"); err != nil {
		t.Errorf("expected valid name to pass, got: %v", err)
	}
}

func TestValidateNonEmpty_Empty(t *testing.T) {
	if err := validateNonEmpty("", "name"); err == nil {
		t.Error("expected error for empty name")
	}
}

func TestValidateNonEmpty_Whitespace(t *testing.T) {
	if err := validateNonEmpty("   ", "name"); err == nil {
		t.Error("expected error for whitespace-only name")
	}
}
