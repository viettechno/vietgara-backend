// Package apidocs serves the REST API's OpenAPI document and a Scalar API
// reference page to read and try it. The document is the one vietgara-proto
// generates from the same google.api.http rules grpc-gateway serves, so the
// reference can never drift from the API.
//
// Both routes live under /api so the API Gateway's /api route already
// reaches them. They are meant for development and staging (API_DOCS_ENABLED).
package apidocs

import (
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
)

const (
	// SpecPath serves the OpenAPI v2 document.
	SpecPath = "/api/openapi.json"
	// DocsPath serves the Scalar API reference page.
	DocsPath = "/api/docs"
)

// scalarScript is pinned so a Scalar release cannot change the page unseen.
const scalarScript = "https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.73.0/dist/browser/standalone.js"

// page loads the document from the same origin, so "Try it" requests go
// to this deployment without a proxy. persistAuth keeps the bearer token
// across reloads.
const page = `<!doctype html>
<html>
  <head>
    <title>VietGara API Reference</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
  </head>
  <body>
    <div id="app"></div>
    <script src="` + scalarScript + `"></script>
    <script>
      Scalar.createApiReference('#app', {
        url: '` + SpecPath + `',
        persistAuth: true,
      })
    </script>
  </body>
</html>
`

// Register adds the document and reference routes to mux.
func Register(mux *runtime.ServeMux, spec []byte) error {
	if err := mux.HandlePath(http.MethodGet, SpecPath, func(w http.ResponseWriter, _ *http.Request, _ map[string]string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(spec)
	}); err != nil {
		return err
	}

	return mux.HandlePath(http.MethodGet, DocsPath, func(w http.ResponseWriter, _ *http.Request, _ map[string]string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	})
}
