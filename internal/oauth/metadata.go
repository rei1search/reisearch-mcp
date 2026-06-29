package oauth

import (
	"encoding/json"
	"net/http"
)

type Metadata struct {
	Resource string `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`

}

func NewMetadataHandler(resource, issuer string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 1. set Content-Type to application/json
		w.Header().Set("Content-Type", "application/json")
		// 2. build a Metadata value from resource + issuer
		metadata := Metadata {
			Resource: resource,
			AuthorizationServers: []string{issuer},
		}
		// 3. encode it to w
		json.NewEncoder(w).Encode(metadata)

	}
}