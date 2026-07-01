package oauth

import (
	"context"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/rei1search/reisearch-mcp/internal/tools"
)

type BearerMiddleware struct {
	metadataURL string
	verifier    *oidc.IDTokenVerifier
}

func NewBearerMiddleware(ctx context.Context, issuer, metadataURL string) (*BearerMiddleware, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	verifier := provider.Verifier(&oidc.Config{SkipClientIDCheck: true})
	return &BearerMiddleware{metadataURL: metadataURL, verifier: verifier}, nil
}

func (m *BearerMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authz := r.Header.Get("Authorization")
		if authz == "" {
			m.challenge(w, "missing bearer token")
			return
		}
		rawToken := strings.TrimPrefix(authz, "Bearer ")
		if _, err := m.verifier.Verify(r.Context(), rawToken); err != nil {
			m.challenge(w, "invalid token")
			return
		}
		ctx := tools.WithToken(r.Context(), rawToken)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *BearerMiddleware) challenge(w http.ResponseWriter, msg string) {
	w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+m.metadataURL+`"`)
	http.Error(w, msg, http.StatusUnauthorized)
}
