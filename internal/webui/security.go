package webui

import (
	"net"
	"net/http"
	"strings"
)

// guard protects the loopback-only UI without an access token, so it can run
// as a long-lived service. It cannot stop other local processes, but it stops
// browsers: the Host check blocks DNS rebinding, and API requests carrying a
// foreign Origin are rejected, which also covers cross-site request forgery
// once the API gains mutations. No CORS headers are sent.
func guard(port string, next http.Handler) http.Handler {
	allowedHosts := map[string]bool{
		net.JoinHostPort("127.0.0.1", port): true,
		net.JoinHostPort("localhost", port): true,
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedHosts[r.Host] {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}

		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")

		if strings.HasPrefix(r.URL.Path, "/api/") && !sameOrigin(r.Header.Get("Origin"), allowedHosts) {
			writeError(w, http.StatusForbidden, "forbidden_origin", "cross-origin API requests are not allowed")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// sameOrigin accepts requests without an Origin header, which browsers omit
// for same-origin GET navigations and fetches, and Origins of the UI itself.
func sameOrigin(origin string, allowedHosts map[string]bool) bool {
	if origin == "" {
		return true
	}

	host, found := strings.CutPrefix(origin, "http://")
	return found && allowedHosts[host]
}
