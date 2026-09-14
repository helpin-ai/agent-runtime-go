package sdk

import (
	"errors"
	"net/url"
	"path"
	"strings"
)

// ValidateModelEndpoint checks structure only. Approval and permission for HTTP
// must come from trusted Runtime app configuration, never from a run flag.
func ValidateModelEndpoint(endpoint *ModelEndpoint) error {
	if endpoint == nil || endpoint.ID == "" || len(endpoint.ID) > 100 {
		return errors.New("a named compatible endpoint is required")
	}
	for _, c := range endpoint.ID {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return errors.New("invalid endpoint ID")
		}
	}
	if endpoint.AuthMode != "api_key" && endpoint.AuthMode != "none" {
		return errors.New("compatible endpoint auth_mode must be api_key or none")
	}
	value := endpoint.BaseURL
	u, err := url.Parse(value)
	if err != nil || len(value) > 2048 || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || u.Opaque != "" ||
		(u.Scheme != "https" && u.Scheme != "http") || strings.TrimSpace(value) != value || strings.HasSuffix(value, "/") ||
		(u.Path != "" && path.Clean(u.Path) != u.Path) {
		return errors.New("endpoint base_url must be a canonical HTTP(S) URL without credentials, query, fragment or trailing slash")
	}
	return nil
}
