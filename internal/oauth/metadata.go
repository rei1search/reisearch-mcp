package oauth

import (
	"context"
	"encoding/json"
	"fmt"
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

// cognitoConfig is the subset of Cognito's openid-configuration we read.
type cognitoConfig struct {
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	JwksURI               string   `json:"jwks_uri"`
	ScopesSupported       []string `json:"scopes_supported"`
}

// NewAuthServerMetadataHandler fetches Cognito's openid-configuration once at
// startup, then serves an RFC 8414 doc whose issuer is our own domain but whose
// endpoints point at Cognito. This lets MCP clients (Claude.ai, ChatGPT)
// discover where to run the authorization-code flow.
func NewAuthServerMetadataHandler(ctx context.Context, resource, issuer string) (http.HandlerFunc, error) {
	configURL := issuer + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, configURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch cognito openid-configuration: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cognito openid-configuration returned %d", resp.StatusCode)
	}

	var cfg cognitoConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode cognito openid-configuration: %w", err)
	}

	metadata := AuthServerMetadata{
		Issuer:                            resource,
		AuthorizationEndpoint:             cfg.AuthorizationEndpoint,
		TokenEndpoint:                     cfg.TokenEndpoint,
		RegistrationEndpoint:              resource + "/register",
		JwksURI:                           cfg.JwksURI,
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic", "client_secret_post", "none"},
		// We advertise the scopes the app client allows, not cfg.ScopesSupported.
		// The pool's openid-configuration lists "profile" too, but the app client
		// does not enable it, so requesting it makes Cognito reject the authorize.
		ScopesSupported: []string{"openid", "email", "phone"},
	}

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(metadata)
	}, nil
}
