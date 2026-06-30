package oauth

import (
	"net/http"
	"strings"
	"sync"
)

// RedirectStore maps an OAuth `state` to the client's real redirect_uri.
//
// We register only our own /callback with Cognito and rewrite every authorize
// request to use it, so Cognito binds each code to our stable callback. The
// /callback relay then needs the client's original redirect_uri to forward the
// code onward — that mapping lives here, keyed by the per-request state.
type RedirectStore struct {
	mu sync.Mutex
	m  map[string]string
}

func NewRedirectStore() *RedirectStore {
	return &RedirectStore{m: map[string]string{}}
}

func (s *RedirectStore) save(state, redirect string) {
	s.mu.Lock()
	s.m[state] = redirect
	s.mu.Unlock()
}

// take returns the stored redirect_uri for a state and removes it, so each
// authorization round-trip consumes its own entry.
func (s *RedirectStore) take(state string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.m[state]
	if ok {
		delete(s.m, state)
	}
	return r, ok
}

// NewCallbackRelayHandler receives Cognito's redirect (code + state) at our own
// /callback and forwards every query parameter on to the client's real
// redirect_uri, which we looked up by state. This lets Cognito keep a single
// stable callback registered while still returning the code to ChatGPT/Claude.
//
// When diag is non-nil (ENABLE_TESTLOGIN), /callback first checks whether the
// state belongs to a /testlogin flow and, if so, completes that flow here
// instead of relaying. diag may be nil in production, in which case /callback
// only relays.
func NewCallbackRelayHandler(store *RedirectStore, diag *DiagFlows) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// A /testlogin flow reuses this same callback (Cognito only allows one).
		// If the state is one of ours, complete it here and stop.
		if diag != nil && diag.TryCallback(w, r) {
			return
		}

		state := r.URL.Query().Get("state")
		clientRedirect, ok := store.take(state)
		if !ok {
			http.Error(w, "unknown or expired state", http.StatusBadRequest)
			return
		}
		sep := "?"
		if strings.Contains(clientRedirect, "?") {
			sep = "&"
		}
		http.Redirect(w, r, clientRedirect+sep+r.URL.RawQuery, http.StatusFound)
	}
}
