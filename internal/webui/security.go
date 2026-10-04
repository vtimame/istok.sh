package webui

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/http"
	"strings"
)

// NewToken returns a random per-process access token for the API.
func NewToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

// guard rejects requests whose Host is not the loopback listener, which blocks
// DNS rebinding, and requires the bearer token on every API request.
func guard(port, token string, next http.Handler) http.Handler {
	allowedHosts := map[string]bool{
		net.JoinHostPort("127.0.0.1", port): true,
		net.JoinHostPort("localhost", port): true,
	}
	expected := []byte("Bearer " + token)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedHosts[r.Host] {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}

		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")

		if strings.HasPrefix(r.URL.Path, "/api/") {
			provided := []byte(r.Header.Get("Authorization"))
			if subtle.ConstantTimeCompare(provided, expected) != 1 {
				writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid access token")
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}
