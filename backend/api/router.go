package api

import (
	"net/http"

	"meeting-assistant/backend/api/gen"
	"meeting-assistant/backend/api/response"
)

// NewRouter creates the HTTP handler for the given Handler: the generated routes
// from api/openapi.yaml, the spec itself, and the logging, CORS and ETag middleware.
func NewRouter(handler *Handler, corsOrigins []string, openAPIPath string) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		http.ServeFile(w, r, openAPIPath)
	})

	gen.HandlerWithOptions(handler, gen.StdHTTPServerOptions{
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			response.Fail(w, http.StatusBadRequest, err.Error())
		},
	})

	return requestLogger(cors(corsOrigins, etag(notFound(mux))))
}
