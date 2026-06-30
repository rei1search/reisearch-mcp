package oauth

import (
	"net/http"
)

// NewAuthorizeProxyHandler rewrites the client's redirect_uri to our own
// /callback before forwarding to Cognito, and remembers the client's original
// redirect_uri keyed by state so the /callback relay can return the code to it.
//
// Cognito binds the authorization code to whatever redirect_uri it sees at
// authorize, and rejects the token exchange (invalid_grant) unless the same
// value comes back. By pinning every flow to our own /callback we (a) only ever
// register one stable callback with Cognito, and (b) redeem with the exact
// redirect_uri we control end-to-end.
func NewAuthorizeProxyHandler(authorizeEndpoint, resource string, store *RedirectStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		clientRedirect := q.Get("redirect_uri")
		state := q.Get("state")

		store.save(state, clientRedirect)
		q.Set("redirect_uri", resource+"/callback")

		// Cognito does not support RFC 8707 Resource Indicators; a forwarded
		// `resource` parameter makes it reject the later token exchange with
		// invalid_grant. Strip it so the code is never bound to it.
		q.Del("resource")

		http.Redirect(w, r, authorizeEndpoint+"?"+q.Encode(), http.StatusFound)
	}
}
