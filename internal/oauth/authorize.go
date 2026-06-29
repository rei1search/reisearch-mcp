package oauth

import (
	"log"
	"net/http"
)

// NewAuthorizeProxyHandler logs the authorization request parameters and then
// redirects the browser to Cognito's real authorize endpoint, preserving the
// query string verbatim. It exists to compare the redirect_uri / code_challenge
// sent at authorize against what arrives at the token exchange, so we can
// diagnose invalid_grant failures.
func NewAuthorizeProxyHandler(authorizeEndpoint string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		log.Printf("authorize proxy: client_id=%q response_type=%q redirect_uri=%q code_challenge=%q code_challenge_method=%q scope=%q",
			q.Get("client_id"),
			q.Get("response_type"),
			q.Get("redirect_uri"),
			q.Get("code_challenge"),
			q.Get("code_challenge_method"),
			q.Get("scope"),
		)

		target := authorizeEndpoint
		if raw := r.URL.RawQuery; raw != "" {
			target += "?" + raw
		}
		http.Redirect(w, r, target, http.StatusFound)
	}
}
