package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
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

// redactForm renders a token form for logging, sorting keys for stable diffs
// and masking secret material so we never log it.
func redactForm(form url.Values) string {
	keys := make([]string, 0, len(form))
	for k := range form {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := strings.Join(form[k], ",")
		switch k {
		case "client_secret", "client_assertion":
			if v != "" {
				v = "[redacted len=" + strconv.Itoa(len(v)) + "]"
			}
		}
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, " ")
}

// NewTokenProxyHandler forwards the authorization-code exchange to Cognito.
//
// MCP clients (Claude.ai) redeem the single-use authorization code more than
// once. Cognito rejects the second redemption with invalid_grant, which the
// client treats as fatal. To absorb that, we cache the first successful
// response keyed by the code and replay it for any later redemption of the
// same code instead of forwarding the now-spent code to Cognito.
//
// It also (a) logs what the client sends and what Cognito returns, and (b)
// injects the client_secret when the client runs a PKCE-only (public) exchange
// but the Cognito app client requires a secret.
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

		log.Printf("token proxy: client body fields: %s", redactForm(form))

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

		// Serialize redemptions of the same code so a duplicate either waits for
		// or replays the first result rather than racing it.
		mu.Lock()
		defer mu.Unlock()

		if code != "" {
			if cached, ok := cache[code]; ok {
				log.Printf("token proxy: replaying cached response for code=%.12s...", code)
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

		log.Printf("token proxy: cognito request fields: %s", redactForm(form))

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

		contentType := resp.Header.Get("Content-Type")
		if code != "" && resp.StatusCode == http.StatusOK {
			cache[code] = cachedToken{status: resp.StatusCode, contentType: contentType, body: respBody}
		}

		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
	}
}
