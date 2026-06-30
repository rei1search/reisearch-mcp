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

// NewDiagCallbackHandler exchanges the code from the self-contained flow by
// redeeming it through OUR OWN /token proxy — exactly the path a real MCP
// client takes — instead of hitting Cognito directly. It redeems TWICE so we
// can see how the proxy's idempotency cache handles a duplicate redemption of
// the single-use code (attempt #2 should replay attempt #1, not invalid_grant).
//
// This is the decisive end-to-end test of the proxy: if attempt #1 returns 200,
// the proxy path is sound and any client failure must be something spending the
// code before our /token sees it; if attempt #1 returns invalid_grant, the bug
// is in our proxy itself.
//
// Note: clientSecret/tokenEndpoint are intentionally unused now — our /token
// proxy injects the secret and forwards to Cognito on our behalf.
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

		// Build the exchange exactly as a public PKCE client would: NO
		// client_secret in the body. Our /token proxy is responsible for
		// injecting the confidential client's secret before reaching Cognito.
		form := url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("client_id", clientID)
		form.Set("code", code)
		form.Set("code_verifier", verifier)
		form.Set("redirect_uri", resource+"/callback")

		proxyTokenURL := resource + "/token"

		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "redeeming code=%.12s... through proxy %s\n\n", code, proxyTokenURL)

		for attempt := 1; attempt <= 2; attempt++ {
			resp, err := http.Post(proxyTokenURL, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
			if err != nil {
				fmt.Fprintf(w, "=== attempt %d: request failed: %v ===\n\n", attempt, err)
				continue
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			fmt.Fprintf(w, "=== attempt %d: status=%d ===\n%s\n\n", attempt, resp.StatusCode, string(body))
		}
	}
}
