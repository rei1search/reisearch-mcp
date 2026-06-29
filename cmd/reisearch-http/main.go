package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rei1search/reisearch-mcp/internal/oauth"
	"github.com/rei1search/reisearch-mcp/internal/reisearch"
	"github.com/rei1search/reisearch-mcp/internal/tools"
)

func main() {
	baseURL := os.Getenv("REISEARCH_PUB_URL")
	resource := os.Getenv("MCP_RESOURCE_URL")
	issuer := os.Getenv("COGNITO_ISSUER")

	if baseURL == "" {
		log.Fatal("Public URL is empty or not defined, Quitting!")
	}
	if resource == "" {
		log.Fatal("resource is empty or not defined, Quitting!")
	}
	if issuer == "" {
		log.Fatal("issuer is empty or not defined, Quitting!")
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

	mux := http.NewServeMux()
	mux.Handle("/.well-known/oauth-protected-resource", oauth.NewMetadataHandler(resource, issuer))
	mux.Handle("/mcp", bearer.Wrap(handler))
	mux.HandleFunc("/", welcome)

	if err := http.ListenAndServe(":4479", mux); err != nil {
		log.Fatal(err)
	}
}

func welcome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("Welcome to ReiSearch MCP server"))
}