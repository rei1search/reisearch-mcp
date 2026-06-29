package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rei1search/reisearch-mcp/internal/oauth"
	"github.com/rei1search/reisearch-mcp/internal/reisearch"
	"github.com/rei1search/reisearch-mcp/internal/tools"
)

func main() {
	baseURL := os.Getenv("REISEARCH_PUB_URL")
	resource := os.Getenv("MCP_RESOURCE_URL")
	issuer := os.Getenv("COGNITO_ISSUER")
	clientID := os.Getenv("OAUTH_CLIENT_ID")
	clientSecret := os.Getenv("OAUTH_CLIENT_SECRET")

	if baseURL == "" {
		log.Fatal("Public URL is empty or not defined, Quitting!")
	}
	if resource == "" {
		log.Fatal("resource is empty or not defined, Quitting!")
	}
	if issuer == "" {
		log.Fatal("issuer is empty or not defined, Quitting!")
	}
	if clientID == "" {
		log.Fatal("OAUTH_CLIENT_ID is empty or not defined, Quitting!")
	}

	metadataURL := resource + "/.well-known/oauth-protected-resource"
	client := reisearch.NewClient(baseURL)

	server := mcp.NewServer(&mcp.Implementation{Name: "reisearch", Version: "v0.1.0"}, nil)
	tools.Register(server, client)

	handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return server
	}, nil)

	bearer, err := oauth.NewBearerMiddleware(context.Background(), issuer, metadataURL)
	if err != nil {
		log.Fatal(err)
	}

	cfg, err := oauth.FetchCognitoConfig(context.Background(), issuer)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/.well-known/oauth-protected-resource", oauth.NewMetadataHandler(resource))
	mux.Handle("/.well-known/oauth-authorization-server", oauth.NewAuthServerMetadataHandler(resource, issuer, cfg))
	mux.Handle("/register", oauth.NewRegistrationHandler(clientID, clientSecret))
	mux.Handle("/token", oauth.NewTokenProxyHandler(cfg.TokenEndpoint, clientID, clientSecret))
	mux.Handle("/mcp", bearer.Wrap(handler))
	mux.HandleFunc("/", welcome)

	log.Printf("reisearch-mcp config: resource=%s issuer=%s api=%s", resource, issuer, baseURL)
	log.Printf("reisearch-mcp listening on :4479")
	if err := http.ListenAndServe(":4479", logRequests(mux)); err != nil {
		log.Fatal(err)
	}
}

// logRequests logs each request without wrapping the ResponseWriter, so the
// MCP handler's SSE streaming (which needs http.Flusher) keeps working.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s %v", r.RemoteAddr, r.Method, r.URL.Path, time.Since(start))
	})
}

func welcome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("Welcome to ReiSearch MCP server"))
}