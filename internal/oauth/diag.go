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

// DiagFlows powers /testlogin: a self-contained, browser-driven PKCE flow that
// lets a developer obtain a real Cognito access token without going through an
// MCP client. It is wired only when ENABLE_TESTLOGIN is set.
//
// /testlogin redirects the browser to Cognito's hosted login (through our own
// /authorize proxy by default). After login, Cognito redirects back to our
// stable /callback; because Cognito only allows that one callback, the test
// flow reuses it. The callback dispatcher (see NewCallbackRelayHandler) hands
// the request here when the state belongs to a test flow, and we redeem the
// code through our own /token proxy — the exact path a real MCP client takes —
// printing the token response so the developer can copy the access token.
type DiagFlows struct {
	resource          string
	clientID          string
	clientSecret      string
	authorizeEndpoint string

	mu    sync.Mutex
	flows map[string]diagFlow
}

// diagFlow records the per-state PKCE verifier plus the toggles chosen at
// /testlogin, so the callback can replicate a real client's exact exchange.
type diagFlow struct {
	verifier  string
	useSecret bool // put client_secret in the token body (mimics ChatGPT/Claude)
}

func NewDiagFlows(resource, clientID, clientSecret, authorizeEndpoint string) *DiagFlows {
	return &DiagFlows{
		resource:          resource,
		clientID:          clientID,
		clientSecret:      clientSecret,
		authorizeEndpoint: authorizeEndpoint,
		flows:             map[string]diagFlow{},
	}
}

func (d *DiagFlows) save(state string, f diagFlow) {
	d.mu.Lock()
	d.flows[state] = f
	d.mu.Unlock()
}

func (d *DiagFlows) take(state string) (diagFlow, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	f, ok := d.flows[state]
	if ok {
		delete(d.flows, state)
	}
	return f, ok
}

func diagRandString() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// LoginHandler serves /testlogin. It starts a fresh PKCE flow and redirects the
// browser to Cognito's hosted login. Query toggles (both default ON) reproduce
// a real client's exact conditions:
//
//	?secret=0   -> do NOT send client_secret in the token body
//	?viaproxy=0 -> hit Cognito's authorize directly instead of our /authorize
func (d *DiagFlows) LoginHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		useSecret := r.URL.Query().Get("secret") != "0"
		viaProxy := r.URL.Query().Get("viaproxy") != "0"

		state := diagRandString()
		verifier := diagRandString()
		sum := sha256.Sum256([]byte(verifier))
		challenge := base64.RawURLEncoding.EncodeToString(sum[:])

		d.save(state, diagFlow{verifier: verifier, useSecret: useSecret})

		params := url.Values{}
		params.Set("client_id", d.clientID)
		params.Set("response_type", "code")
		params.Set("scope", "openid email phone")
		params.Set("redirect_uri", d.resource+"/callback")
		params.Set("state", state)
		params.Set("code_challenge", challenge)
		params.Set("code_challenge_method", "S256")

		authzBase := d.authorizeEndpoint
		if viaProxy {
			authzBase = d.resource + "/authorize"
		}

		http.Redirect(w, r, authzBase+"?"+params.Encode(), http.StatusFound)
	}
}

// TryCallback completes a /testlogin flow if `state` belongs to one. It returns
// true when it handled the request, so the relay knows not to run as well.
//
// It redeems the code TWICE through our /token proxy to exercise the
// idempotency cache: attempt #2 should replay attempt #1 rather than fail with
// invalid_grant. The token response (including the access token) is written to
// the page so the developer can copy it for testing /mcp and the backend.
func (d *DiagFlows) TryCallback(w http.ResponseWriter, r *http.Request) bool {
	q := r.URL.Query()
	state := q.Get("state")

	flow, ok := d.take(state)
	if !ok {
		return false
	}

	w.Header().Set("Content-Type", "text/plain")

	if errParam := q.Get("error"); errParam != "" {
		fmt.Fprintf(w, "authorize error: %s\n%s", errParam, q.Get("error_description"))
		return true
	}

	code := q.Get("code")
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", d.clientID)
	form.Set("code", code)
	form.Set("code_verifier", flow.verifier)
	form.Set("redirect_uri", d.resource+"/callback")
	if flow.useSecret {
		form.Set("client_secret", d.clientSecret)
	}

	proxyTokenURL := d.resource + "/token"
	fmt.Fprintf(w, "redeeming code=%.12s... through proxy %s (secret_in_body=%t)\n\n", code, proxyTokenURL, flow.useSecret)

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
	return true
}
