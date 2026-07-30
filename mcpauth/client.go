// Package mcpauth provides headless, app-side OAuth helpers for remote MCP
// installations. Applications retain ownership of browser routes, workspace
// authorization, durable state, encryption, refresh tokens, and notifications.
package mcpauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	maxResponseBytes = 1 << 20
	maxURLBytes      = 4096
	maxClientIDBytes = 4096
	maxSecretBytes   = 64 << 10
)

var (
	resourceMetadataPattern = regexp.MustCompile(`(?i)resource_metadata="([^"]+)"`)
	pkceVerifierPattern     = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
)

type ProtectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

type AuthorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	ScopesSupported                   []string `json:"scopes_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
}

type Configuration struct {
	Resource      ProtectedResourceMetadata
	Authorization AuthorizationServerMetadata
}

type ClientRegistration struct {
	ClientID                string `json:"client_id"`
	ClientSecret            string `json:"client_secret,omitempty"`
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method,omitempty"`
}

type Token struct {
	AccessToken  string     `json:"access_token"`
	RefreshToken string     `json:"refresh_token,omitempty"`
	TokenType    string     `json:"token_type,omitempty"`
	ExpiresIn    int64      `json:"expires_in,omitempty"`
	Scope        string     `json:"scope,omitempty"`
	ExpiresAt    *time.Time `json:"-"`
}

// AuthorizationRequest contains the browser URL plus the values an app must
// bind to its user/workspace installation and store until callback completion.
type AuthorizationRequest struct {
	URL      string
	State    string
	Verifier string
}

// RemoteError intentionally excludes remote bodies, headers, and secrets.
type RemoteError struct {
	Operation  string
	StatusCode int
	Code       string
}

func (e *RemoteError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("MCP OAuth %s failed with HTTP %d", e.Operation, e.StatusCode)
	}
	return fmt.Sprintf("MCP OAuth %s failed", e.Operation)
}

func IsStatus(err error, status int) bool {
	var remote *RemoteError
	return errors.As(err, &remote) && remote.StatusCode == status
}

type URLValidator func(*url.URL) error

type Client struct {
	resource   *url.URL
	httpClient *http.Client
	allowed    []string
	allowLocal bool
	validator  URLValidator
}

type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	allowed    []string
	allowLocal bool
	validator  URLValidator
}

func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithAllowedHosts adds exact hosts or *.example.com suffix patterns for
// discovered authorization URLs. The resource host is always included.
func WithAllowedHosts(hosts ...string) Option {
	return func(options *clientOptions) { options.allowed = append(options.allowed, hosts...) }
}

// WithURLValidator adds application-specific URL policy after scheme and host
// allowlist validation, for example an egress-policy or DNS ownership check.
func WithURLValidator(validator URLValidator) Option {
	return func(options *clientOptions) { options.validator = validator }
}

// WithInsecureLocalhost is intended only for local development and tests.
func WithInsecureLocalhost() Option {
	return func(options *clientOptions) { options.allowLocal = true }
}

func NewClient(resourceURL string, options ...Option) (*Client, error) {
	settings := clientOptions{}
	for _, option := range options {
		if option != nil {
			option(&settings)
		}
	}
	resource, err := parseURL(resourceURL, settings.allowLocal)
	if err != nil || resource.RawQuery != "" {
		return nil, fmt.Errorf("MCP resource URL is invalid")
	}
	settings.allowed = append(settings.allowed, resource.Hostname())
	client := &Client{resource: resource, allowed: settings.allowed, allowLocal: settings.allowLocal, validator: settings.validator}
	if settings.httpClient != nil {
		clone := *settings.httpClient
		clone.CheckRedirect = client.redirectPolicy
		client.httpClient = &clone
	} else {
		client.httpClient = client.secureHTTPClient()
	}
	return client, nil
}

func (c *Client) redirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 {
		return fmt.Errorf("MCP OAuth redirect limit exceeded")
	}
	if len(via) == 0 || !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
		return fmt.Errorf("MCP OAuth cross-host redirect is not allowed")
	}
	return c.ValidateURL(req.URL.String())
}

func (c *Client) ResourceURL() string { return c.resource.String() }

func (c *Client) ValidateURL(raw string) error {
	parsed, err := parseURL(raw, c.allowLocal)
	if err != nil {
		return err
	}
	if !hostAllowed(parsed.Hostname(), c.allowed) {
		return fmt.Errorf("MCP OAuth URL host is not allowed")
	}
	if c.validator != nil {
		return c.validator(parsed)
	}
	return nil
}

func (c *Client) Discover(ctx context.Context) (*Configuration, error) {
	candidates := make([]string, 0, 3)
	if challenge := c.challengeResourceMetadata(ctx); challenge != "" {
		candidates = append(candidates, challenge)
	}
	candidates = append(candidates, protectedResourceWellKnown(c.resource, true), protectedResourceWellKnown(c.resource, false))

	var resource ProtectedResourceMetadata
	found := false
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if seen[candidate] || c.ValidateURL(candidate) != nil {
			continue
		}
		seen[candidate] = true
		if err := c.getJSON(ctx, candidate, &resource); err == nil && len(resource.AuthorizationServers) > 0 {
			found = true
			break
		}
	}
	if !found {
		return nil, &RemoteError{Operation: "protected-resource discovery", Code: "metadata_unavailable"}
	}
	if resource.Resource == "" {
		resource.Resource = c.ResourceURL()
	}
	if err := c.ValidateURL(resource.Resource); err != nil {
		return nil, &RemoteError{Operation: "resource validation", Code: "metadata_invalid"}
	}
	issuer := strings.TrimSpace(resource.AuthorizationServers[0])
	if err := c.ValidateURL(issuer); err != nil {
		return nil, &RemoteError{Operation: "authorization-server validation", Code: "metadata_invalid"}
	}
	issuerURL, _ := url.Parse(issuer)
	metadataURLs := []string{
		authorizationServerWellKnown(issuerURL, "oauth-authorization-server"),
		authorizationServerWellKnown(issuerURL, "openid-configuration"),
	}
	var authorization AuthorizationServerMetadata
	found = false
	for _, candidate := range metadataURLs {
		if c.ValidateURL(candidate) != nil {
			continue
		}
		if err := c.getJSON(ctx, candidate, &authorization); err == nil && authorization.AuthorizationEndpoint != "" && authorization.TokenEndpoint != "" {
			found = true
			break
		}
	}
	if !found || c.ValidateURL(authorization.AuthorizationEndpoint) != nil || c.ValidateURL(authorization.TokenEndpoint) != nil {
		return nil, &RemoteError{Operation: "authorization-server discovery", Code: "metadata_unavailable"}
	}
	if authorization.Issuer == "" || !sameIssuer(issuer, authorization.Issuer) {
		return nil, &RemoteError{Operation: "authorization-server discovery", Code: "issuer_mismatch"}
	}
	if authorization.RegistrationEndpoint != "" && c.ValidateURL(authorization.RegistrationEndpoint) != nil {
		authorization.RegistrationEndpoint = ""
	}
	if len(authorization.CodeChallengeMethodsSupported) > 0 && !containsFold(authorization.CodeChallengeMethodsSupported, "S256") {
		return nil, &RemoteError{Operation: "PKCE validation", Code: "pkce_s256_required"}
	}
	return &Configuration{Resource: resource, Authorization: authorization}, nil
}

func (c *Client) Register(ctx context.Context, endpoint, redirectURI string) (*ClientRegistration, error) {
	if endpoint == "" || c.ValidateURL(endpoint) != nil {
		return nil, &RemoteError{Operation: "dynamic client registration", Code: "registration_unavailable"}
	}
	if err := validateRedirectURI(redirectURI, c.allowLocal); err != nil {
		return nil, &RemoteError{Operation: "dynamic client registration", Code: "redirect_uri_invalid"}
	}
	payload := map[string]any{
		"client_name": "Agent Runtime host app", "redirect_uris": []string{redirectURI},
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
		"token_endpoint_auth_method": "none",
	}
	var registration ClientRegistration
	if err := c.postJSON(ctx, endpoint, payload, &registration); err != nil {
		return nil, err
	}
	registration.ClientID = strings.TrimSpace(registration.ClientID)
	registration.TokenEndpointAuthMethod = strings.TrimSpace(registration.TokenEndpointAuthMethod)
	if registration.ClientID == "" || len(registration.ClientID) > maxClientIDBytes || len(registration.ClientSecret) > maxSecretBytes {
		return nil, &RemoteError{Operation: "dynamic client registration", Code: "registration_invalid"}
	}
	if registration.TokenEndpointAuthMethod == "" {
		registration.TokenEndpointAuthMethod = "none"
	}
	if !validClientAuthMethod(registration.TokenEndpointAuthMethod) {
		return nil, &RemoteError{Operation: "dynamic client registration", Code: "client_auth_unsupported"}
	}
	if registration.TokenEndpointAuthMethod != "none" && registration.ClientSecret == "" {
		return nil, &RemoteError{Operation: "dynamic client registration", Code: "registration_invalid"}
	}
	return &registration, nil
}

func (c *Client) NewAuthorizationRequest(configuration *Configuration, clientID, redirectURI string, scopes []string) (*AuthorizationRequest, error) {
	if configuration == nil || c.ValidateURL(configuration.Authorization.AuthorizationEndpoint) != nil {
		return nil, fmt.Errorf("MCP OAuth configuration is invalid")
	}
	clientID = strings.TrimSpace(clientID)
	if clientID == "" || len(clientID) > maxClientIDBytes {
		return nil, fmt.Errorf("MCP OAuth client ID is invalid")
	}
	if err := validateRedirectURI(redirectURI, c.allowLocal); err != nil {
		return nil, err
	}
	state, err := randomSecret(32)
	if err != nil {
		return nil, err
	}
	verifier, err := randomSecret(64)
	if err != nil {
		return nil, err
	}
	resource := strings.TrimSpace(configuration.Resource.Resource)
	if resource == "" {
		resource = c.ResourceURL()
	}
	if err := c.ValidateURL(resource); err != nil {
		return nil, fmt.Errorf("MCP OAuth resource is invalid")
	}
	if err := validateScopes(scopes); err != nil {
		return nil, err
	}
	requestURL, err := authorizationURL(configuration.Authorization.AuthorizationEndpoint, clientID, redirectURI, state, verifier, resource, scopes)
	if err != nil {
		return nil, err
	}
	return &AuthorizationRequest{URL: requestURL, State: state, Verifier: verifier}, nil
}

func (c *Client) ExchangeCode(ctx context.Context, endpoint, clientID, clientSecret, authMethod, code, verifier, redirectURI, resource string) (*Token, error) {
	if strings.TrimSpace(code) == "" || !pkceVerifierPattern.MatchString(verifier) {
		return nil, &RemoteError{Operation: "authorization code validation", Code: "callback_invalid"}
	}
	if err := validateRedirectURI(redirectURI, c.allowLocal); err != nil {
		return nil, &RemoteError{Operation: "authorization code validation", Code: "redirect_uri_invalid"}
	}
	values := url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID},
		"redirect_uri": {redirectURI}, "code_verifier": {verifier}, "resource": {resource},
	}
	return c.tokenRequest(ctx, endpoint, clientID, clientSecret, authMethod, values)
}

func (c *Client) Refresh(ctx context.Context, endpoint, clientID, clientSecret, authMethod, refreshToken, resource string) (*Token, error) {
	if strings.TrimSpace(refreshToken) == "" || len(refreshToken) > maxSecretBytes {
		return nil, &RemoteError{Operation: "refresh token validation", Code: "refresh_token_invalid"}
	}
	values := url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {clientID}, "resource": {resource},
	}
	return c.tokenRequest(ctx, endpoint, clientID, clientSecret, authMethod, values)
}

func HashState(state string) string {
	hash := sha256.Sum256([]byte(state))
	return hex.EncodeToString(hash[:])
}

func (c *Client) tokenRequest(ctx context.Context, endpoint, clientID, clientSecret, authMethod string, values url.Values) (*Token, error) {
	clientID = strings.TrimSpace(clientID)
	authMethod = strings.TrimSpace(authMethod)
	if clientID == "" || len(clientID) > maxClientIDBytes || len(clientSecret) > maxSecretBytes || c.ValidateURL(endpoint) != nil || c.ValidateURL(values.Get("resource")) != nil || !validClientAuthMethod(authMethod) {
		return nil, &RemoteError{Operation: "token endpoint validation", Code: "metadata_invalid"}
	}
	if authMethod != "" && authMethod != "none" && clientSecret == "" {
		return nil, &RemoteError{Operation: "token endpoint validation", Code: "client_secret_required"}
	}
	if authMethod == "client_secret_post" && clientSecret != "" {
		values.Set("client_secret", clientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, &RemoteError{Operation: "token request"}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if authMethod == "client_secret_basic" && clientSecret != "" {
		req.SetBasicAuth(clientID, clientSecret)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &RemoteError{Operation: "token request"}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &RemoteError{Operation: "token request", StatusCode: resp.StatusCode, Code: "token_rejected"}
	}
	var token Token
	if err := decodeJSON(resp.Body, &token); err != nil || token.AccessToken == "" || len(token.AccessToken) > 64<<10 || len(token.RefreshToken) > 64<<10 {
		return nil, &RemoteError{Operation: "token response", Code: "token_invalid"}
	}
	if token.TokenType != "" && !strings.EqualFold(token.TokenType, "Bearer") {
		return nil, &RemoteError{Operation: "token response", Code: "token_type_unsupported"}
	}
	if token.ExpiresIn > 0 {
		expiresAt := time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second)
		token.ExpiresAt = &expiresAt
	}
	return &token, nil
}

func (c *Client) challengeResourceMetadata(ctx context.Context) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.ResourceURL(), nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	match := resourceMetadataPattern.FindStringSubmatch(resp.Header.Get("WWW-Authenticate"))
	if len(match) != 2 {
		return ""
	}
	decoded, err := url.QueryUnescape(match[1])
	if err != nil {
		return ""
	}
	return decoded
}

func (c *Client) getJSON(ctx context.Context, endpoint string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &RemoteError{Operation: "metadata discovery", StatusCode: resp.StatusCode}
	}
	return decodeJSON(resp.Body, target)
}

func (c *Client) postJSON(ctx context.Context, endpoint string, payload, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &RemoteError{Operation: "dynamic client registration"}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &RemoteError{Operation: "dynamic client registration", StatusCode: resp.StatusCode}
	}
	return decodeJSON(resp.Body, target)
}

func decodeJSON(reader io.Reader, target any) error {
	payload, err := io.ReadAll(io.LimitReader(reader, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(payload) > maxResponseBytes {
		return fmt.Errorf("MCP OAuth response exceeds %d bytes", maxResponseBytes)
	}
	return json.Unmarshal(payload, target)
}

func authorizationURL(endpoint, clientID, redirectURI, state, verifier, resource string, scopes []string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("invalid OAuth authorization endpoint")
	}
	challenge := sha256.Sum256([]byte(verifier))
	query := parsed.Query()
	query.Set("response_type", "code")
	query.Set("client_id", clientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("state", state)
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	query.Set("code_challenge_method", "S256")
	query.Set("resource", resource)
	if len(scopes) > 0 {
		query.Set("scope", strings.Join(scopes, " "))
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func protectedResourceWellKnown(resource *url.URL, includePath bool) string {
	value := *resource
	path := ""
	if includePath {
		path = strings.TrimPrefix(strings.TrimSuffix(resource.Path, "/"), "/")
	}
	value.Path = "/.well-known/oauth-protected-resource"
	if path != "" {
		value.Path += "/" + path
	}
	value.RawPath, value.RawQuery, value.Fragment = "", "", ""
	return value.String()
}

func authorizationServerWellKnown(issuer *url.URL, kind string) string {
	value := *issuer
	value.Path = "/.well-known/" + kind + strings.TrimSuffix(issuer.Path, "/")
	value.RawPath, value.RawQuery, value.Fragment = "", "", ""
	return value.String()
}

func parseURL(raw string, allowLocal bool) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || len(raw) > maxURLBytes || !parsed.IsAbs() || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, fmt.Errorf("MCP OAuth URL is invalid")
	}
	if parsed.Scheme != "https" && !(allowLocal && parsed.Scheme == "http" && isLocalHostname(parsed.Hostname())) {
		return nil, fmt.Errorf("MCP OAuth URL must use HTTPS")
	}
	return parsed, nil
}

func validateRedirectURI(raw string, allowLocal bool) error {
	if _, err := parseURL(raw, allowLocal); err != nil {
		return fmt.Errorf("MCP OAuth redirect URI must be an absolute HTTPS URL without a fragment")
	}
	return nil
}

func validateScopes(scopes []string) error {
	total := 0
	for _, scope := range scopes {
		if scope == "" || strings.TrimSpace(scope) != scope || len(strings.Fields(scope)) != 1 || strings.ContainsAny(scope, "\x00\r\n") {
			return fmt.Errorf("MCP OAuth scope is invalid")
		}
		total += len(scope)
	}
	if total > 32<<10 {
		return fmt.Errorf("MCP OAuth scopes are too large")
	}
	return nil
}

func sameIssuer(expected, actual string) bool {
	expectedURL, expectedErr := url.Parse(strings.TrimSpace(expected))
	actualURL, actualErr := url.Parse(strings.TrimSpace(actual))
	if expectedErr != nil || actualErr != nil || expectedURL.RawQuery != "" || actualURL.RawQuery != "" || expectedURL.Fragment != "" || actualURL.Fragment != "" {
		return false
	}
	return strings.EqualFold(expectedURL.Scheme, actualURL.Scheme) &&
		strings.EqualFold(expectedURL.Host, actualURL.Host) &&
		strings.TrimSuffix(expectedURL.EscapedPath(), "/") == strings.TrimSuffix(actualURL.EscapedPath(), "/")
}

func validClientAuthMethod(value string) bool {
	switch strings.TrimSpace(value) {
	case "", "none", "client_secret_basic", "client_secret_post":
		return true
	default:
		return false
	}
}

func hostAllowed(host string, allowed []string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	for _, pattern := range allowed {
		pattern = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(pattern), "."))
		if host == pattern || strings.HasPrefix(pattern, "*.") && strings.HasSuffix(host, pattern[1:]) && host != pattern[2:] {
			return true
		}
	}
	return false
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func randomSecret(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func isLocalHostname(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	ip := net.ParseIP(host)
	return host == "localhost" || strings.HasSuffix(host, ".localhost") || ip != nil && ip.IsLoopback()
}

func isNonPublicIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() ||
		ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	_, shared, _ := net.ParseCIDR("100.64.0.0/10")
	return shared.Contains(ip)
}

func (c *Client) secureHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid MCP OAuth outbound address")
		}
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(addrs) == 0 {
			return nil, fmt.Errorf("MCP OAuth host could not be resolved")
		}
		local := c.allowLocal && isLocalHostname(host)
		for _, addr := range addrs {
			if (local && !addr.IP.IsLoopback()) || (!local && isNonPublicIP(addr.IP)) {
				continue
			}
			if conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(addr.IP.String(), port)); dialErr == nil {
				return conn, nil
			}
		}
		return nil, fmt.Errorf("MCP OAuth host has no allowed reachable address")
	}
	return &http.Client{
		Transport: transport, Timeout: 30 * time.Second,
		CheckRedirect: c.redirectPolicy,
	}
}
