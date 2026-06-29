package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// Diagnostic, self-contained PKCE flow that cuts the MCP client out entirely.
// Visiting /testlogin runs authorize against Cognito with our own PKCE pair and
// /callback exchanges the code, so we can tell whether Cognito itself can
// complete authorize -> token for this app client.

var (
	diagMu        sync.Mutex
	diagVerifiers = map[string]string{}
)

func randString() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// NewDiagLoginHandler starts the self-contained PKCE flow.
func NewDiagLoginHandler(resource, clientID, authorizeEndpoint string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state := randString()
		verifier := randString()
		sum := sha256.Sum256([]byte(verifier))
		challenge := base64.RawURLEncoding.EncodeToString(sum[:])

		diagMu.Lock()
		diagVerifiers[state] = verifier
		diagMu.Unlock()

		params := url.Values{}
		params.Set("client_id", clientID)
		params.Set("response_type", "code")
		params.Set("scope", "openid email phone")
		params.Set("redirect_uri", resource+"/callback")
		params.Set("state", state)
		params.Set("code_challenge", challenge)
		params.Set("code_challenge_method", "S256")

		http.Redirect(w, r, authorizeEndpoint+"?"+params.Encode(), http.StatusFound)
	}
}

// NewDiagCallbackHandler exchanges the code from the self-contained flow and
// renders Cognito's raw response.
func NewDiagCallbackHandler(resource, clientID, clientSecret, tokenEndpoint string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		code := q.Get("code")
		state := q.Get("state")

		if errParam := q.Get("error"); errParam != "" {
			fmt.Fprintf(w, "authorize error: %s\n%s", errParam, q.Get("error_description"))
			return
		}

		diagMu.Lock()
		verifier := diagVerifiers[state]
		delete(diagVerifiers, state)
		diagMu.Unlock()

		if verifier == "" {
			fmt.Fprintf(w, "no verifier for state %q (start at /testlogin)", state)
			return
		}

		form := url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("client_id", clientID)
		if clientSecret != "" {
			form.Set("client_secret", clientSecret)
		}
		form.Set("code", code)
		form.Set("code_verifier", verifier)
		form.Set("redirect_uri", resource+"/callback")

		resp, err := http.Post(tokenEndpoint, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
		if err != nil {
			fmt.Fprintf(w, "exchange request failed: %v", err)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)

		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "token exchange status=%d\n\n%s", resp.StatusCode, string(body))
	}
}
