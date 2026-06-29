package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
)

// NewTokenProxyHandler forwards the authorization-code exchange to Cognito. It
// exists so we can (a) log exactly what the MCP client sends and what Cognito
// returns, and (b) inject the client_secret when the client runs a PKCE-only
// (public) exchange but the Cognito app client requires a secret.
func NewTokenProxyHandler(tokenEndpoint, clientID, clientSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}

		form, _ := url.ParseQuery(string(body))
		hasAuthHeader := r.Header.Get("Authorization") != ""

		code := form.Get("code")
		verifier := form.Get("code_verifier")
		var derivedChallenge string
		if verifier != "" {
			sum := sha256.Sum256([]byte(verifier))
			derivedChallenge = base64.RawURLEncoding.EncodeToString(sum[:])
		}
		log.Printf("token proxy: grant=%s client_id=%q has_secret_in_body=%t has_basic_auth=%t redirect_uri=%q code=%.12s... derived_challenge=%s",
			form.Get("grant_type"),
			form.Get("client_id"),
			form.Get("client_secret") != "",
			hasAuthHeader,
			form.Get("redirect_uri"),
			code,
			derivedChallenge,
		)

		// Cognito's app client is confidential; if the client didn't send the
		// secret (PKCE public flow) inject it so Cognito accepts the exchange.
		if clientSecret != "" && form.Get("client_secret") == "" && !hasAuthHeader {
			if form.Get("client_id") == "" {
				form.Set("client_id", clientID)
			}
			form.Set("client_secret", clientSecret)
		}

		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
		if err != nil {
			http.Error(w, "build request", http.StatusInternalServerError)
			return
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if hasAuthHeader {
			req.Header.Set("Authorization", r.Header.Get("Authorization"))
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, "forward to cognito", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("token proxy: cognito status=%d body=%s", resp.StatusCode, string(respBody))

		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
	}
}
