package oauth

import (
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// cachedToken is a successful token response we replay for duplicate exchanges
// of the same single-use authorization code.
type cachedToken struct {
	status      int
	contentType string
	body        []byte
}

// NewTokenProxyHandler forwards the authorization-code exchange to Cognito.
//
// MCP clients redeem the single-use authorization code more than once. Cognito
// rejects the second redemption with invalid_grant, which the client treats as
// fatal. To absorb that, we cache the first successful response keyed by the
// code and replay it for any later redemption of the same code.
//
// It also (a) strips parameters Cognito rejects, (b) pins redirect_uri to our
// own /callback (the value the code was bound to), and (c) injects the
// client_secret for public PKCE clients that don't send one.
func NewTokenProxyHandler(tokenEndpoint, clientID, clientSecret, resource string) http.HandlerFunc {
	var (
		mu    sync.Mutex
		cache = map[string]cachedToken{}
	)

	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}

		form, _ := url.ParseQuery(string(body))
		hasAuthHeader := r.Header.Get("Authorization") != ""

		// Cognito does not support RFC 8707 Resource Indicators. Clients like
		// ChatGPT send a `resource` parameter, which Cognito rejects with
		// invalid_grant. Strip it before forwarding.
		form.Del("resource")

		// The code was bound to our own /callback at authorize (see the
		// authorize proxy), so redeem with that exact value. The client sends
		// its own redirect_uri (e.g. chatgpt.com/...), which would mismatch and
		// make Cognito return invalid_grant.
		if form.Get("grant_type") == "authorization_code" {
			form.Set("redirect_uri", resource+"/callback")
		}

		code := form.Get("code")

		// Serialize redemptions of the same code so a duplicate either waits for
		// or replays the first result rather than racing it.
		mu.Lock()
		defer mu.Unlock()

		if code != "" {
			if cached, ok := cache[code]; ok {
				w.Header().Set("Content-Type", cached.contentType)
				w.WriteHeader(cached.status)
				w.Write(cached.body)
				return
			}
		}

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
		// Don't log successful bodies; they contain access/refresh tokens.
		if resp.StatusCode != http.StatusOK {
			log.Printf("token proxy: cognito status=%d body=%s", resp.StatusCode, string(respBody))
		}

		contentType := resp.Header.Get("Content-Type")
		if code != "" && resp.StatusCode == http.StatusOK {
			cache[code] = cachedToken{status: resp.StatusCode, contentType: contentType, body: respBody}
		}

		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
	}
}
