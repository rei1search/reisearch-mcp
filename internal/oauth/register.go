package oauth

import (
	"encoding/json"
	"net/http"
	"time"
)

// registrationRequest is the subset of the RFC 7591 client metadata we read
// from the incoming registration request. We only echo redirect_uris back.
type registrationRequest struct {
	RedirectURIs []string `json:"redirect_uris"`
}

// registrationResponse is an RFC 7591 client registration response.
type registrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	RedirectURIs            []string `json:"redirect_uris,omitempty"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

// NewRegistrationHandler implements a Dynamic Client Registration (RFC 7591)
// shim. Cognito does not support DCR, but MCP clients like Claude.ai require a
// registration_endpoint. Rather than registering a new client, we hand back our
// pre-created Cognito app client ID so the client can run the normal
// authorization-code + PKCE flow.
//
// We register clients as PUBLIC PKCE clients (token_endpoint_auth_method=none)
// and never return the Cognito client_secret. The confidential secret is held
// only by the server and injected by the /token proxy. Returning it here would
// both leak the secret and push some clients into a confidential-client token
// exchange that diverges from the public PKCE path we proxy.
//
// The Cognito app client (clientID) must already list the caller's redirect_uri
// in its allowed callback URLs, or Cognito will reject the authorize request.
func NewRegistrationHandler(clientID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req registrationRequest
		json.NewDecoder(r.Body).Decode(&req)

		resp := registrationResponse{
			ClientID:                clientID,
			ClientIDIssuedAt:        time.Now().Unix(),
			RedirectURIs:            req.RedirectURIs,
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			ResponseTypes:           []string{"code"},
			TokenEndpointAuthMethod: "none",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(resp)
	}
}
