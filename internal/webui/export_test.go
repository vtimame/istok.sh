package webui

import "net/http"

// NewTestHandler builds the guarded handler exactly as Listen does, without
// binding a socket, so external tests can drive it with httptest.
func NewTestHandler(port string, services Services) http.Handler {
	mux := http.NewServeMux()
	api{services: services}.register(mux)
	mux.Handle("/", assetsHandler())

	return guard(port, mux)
}
