package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Metadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
}

// NewMetadataHandler serves RFC 9728 protected-resource metadata. We advertise
// ourselves (resource) as the authorization server so clients fetch our
// /.well-known/oauth-authorization-server doc, which proxies Cognito's real
// endpoints. Cognito itself does not serve oauth-authorization-server metadata.
func NewMetadataHandler(resource string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		metadata := Metadata{
			Resource:             resource,
			AuthorizationServers: []string{resource},
		}
		json.NewEncoder(w).Encode(metadata)
	}
}

// AuthServerMetadata is the subset of RFC 8414 fields MCP clients need to drive
// the authorization-code + PKCE flow.
type AuthServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	JwksURI                           string   `json:"jwks_uri"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
}

// CognitoConfig is the subset of Cognito's openid-configuration we use.
type CognitoConfig struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JwksURI               string `json:"jwks_uri"`
}

// FetchCognitoConfig reads Cognito's openid-configuration once at startup so we
// can reuse its real endpoints across our metadata, token-proxy and JWKS.
func FetchCognitoConfig(ctx context.Context, issuer string) (CognitoConfig, error) {
	var cfg CognitoConfig
	configURL := issuer + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, configURL, nil)
	if err != nil {
		return cfg, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return cfg, fmt.Errorf("fetch cognito openid-configuration: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return cfg, fmt.Errorf("cognito openid-configuration returned %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("decode cognito openid-configuration: %w", err)
	}
	return cfg, nil
}

// NewAuthServerMetadataHandler serves an RFC 8414 doc. The authorization
// endpoint points straight at Cognito, but the token endpoint points back at
// our own /token proxy so we can observe and adjust the exchange.
func NewAuthServerMetadataHandler(resource, issuer string, cfg CognitoConfig) http.HandlerFunc {
	metadata := AuthServerMetadata{
		// Advertise ourselves as the issuer so the client discovers ONLY our
		// endpoints. If we advertised Cognito's issuer, the client does its own
		// OIDC discovery on Cognito and redeems the single-use code directly
		// there (bypassing our /token), causing a duplicate redemption.
		Issuer:                resource,
		AuthorizationEndpoint: resource + "/authorize",
		TokenEndpoint:         resource + "/token",
		RegistrationEndpoint:  resource + "/register",
		// Point at our own JWKS proxy, not Cognito's host. Any reference to the
		// Cognito domain in discovery lets a client derive Cognito's real token
		// endpoint and redeem the single-use code there directly, bypassing our
		// /token proxy and spending the code before our exchange runs.
		JwksURI:                       resource + "/jwks",
		ResponseTypesSupported:        []string{"code"},
		GrantTypesSupported:           []string{"authorization_code", "refresh_token"},
		CodeChallengeMethodsSupported: []string{"S256"},
		// Public PKCE only: clients send no secret and our /token proxy injects
		// the confidential Cognito secret server-side. Advertising client_secret_*
		// would invite clients into a confidential exchange we don't want them to run.
		TokenEndpointAuthMethodsSupported: []string{"none"},
		// We advertise the scopes the app client allows, not the pool's full
		// list. The pool also lists "profile", but the app client does not
		// enable it, so requesting it makes Cognito reject the authorize.
		ScopesSupported: []string{"openid", "email", "phone"},
	}

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(metadata)
	}
}

// OpenIDConfig mirrors our AuthServerMetadata as an OIDC discovery document.
// Some clients fetch /.well-known/openid-configuration instead of (or in
// addition to) oauth-authorization-server. If that 404s, they fall back to
// deriving the issuer from our jwks_uri host and discover Cognito's real
// endpoints. Serving our own doc keeps every endpoint pointed at us.
type OpenIDConfig struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	JwksURI                           string   `json:"jwks_uri"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	SubjectTypesSupported             []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported  []string `json:"id_token_signing_alg_values_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
}

// NewOpenIDConfigHandler serves an OIDC discovery doc with every endpoint on
// our own domain, so a client fetching openid-configuration never 404s into a
// fallback that exposes Cognito's real endpoints.
func NewOpenIDConfigHandler(resource string) http.HandlerFunc {
	cfg := OpenIDConfig{
		Issuer:                            resource,
		AuthorizationEndpoint:             resource + "/authorize",
		TokenEndpoint:                     resource + "/token",
		RegistrationEndpoint:              resource + "/register",
		JwksURI:                           resource + "/jwks",
		ResponseTypesSupported:            []string{"code"},
		SubjectTypesSupported:             []string{"public"},
		IDTokenSigningAlgValuesSupported:  []string{"RS256"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		ScopesSupported:                   []string{"openid", "email", "phone"},
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cfg)
	}
}

// NewJWKSProxyHandler fetches Cognito's JWKS and serves it from our own domain,
// so no discovery document ever references the Cognito host. This closes the
// only remaining path by which a client could find Cognito's real endpoints and
// redeem the single-use authorization code directly, bypassing our /token.
func NewJWKSProxyHandler(cognitoJwksURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := http.Get(cognitoJwksURL)
		if err != nil {
			http.Error(w, "fetch jwks", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
	}
}
