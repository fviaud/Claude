package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

// apiKeys holds the SHA-256 hashes of the accepted keys. Comparing
// fixed-size hashes in constant time avoids leaking a key or its length
// through response timing.
type apiKeys [][sha256.Size]byte

func newAPIKeys(keys []string) apiKeys {
	hashes := make(apiKeys, 0, len(keys))
	for _, k := range keys {
		hashes = append(hashes, sha256.Sum256([]byte(k)))
	}
	return hashes
}

// valid reports whether key matches one of the accepted keys. Every key is
// checked so the time taken does not depend on which one matches.
func (a apiKeys) valid(key string) bool {
	if key == "" {
		return false
	}
	h := sha256.Sum256([]byte(key))
	ok := 0
	for i := range a {
		ok |= subtle.ConstantTimeCompare(h[:], a[i][:])
	}
	return ok == 1
}

// requestKey returns the key sent in the X-API-Key header or, failing that,
// as "Authorization: Bearer <key>".
func requestKey(r *http.Request) string {
	if k := r.Header.Get("X-API-Key"); k != "" {
		return k
	}
	const prefix = "Bearer "
	if h := r.Header.Get("Authorization"); len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}

// requireAPIKey rejects requests without a valid key with 401.
func (s *Server) requireAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.keys.valid(requestKey(r)) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="catalogue"`)
			writeError(w, http.StatusUnauthorized, "missing or invalid API key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ParseAPIKeys splits a comma-separated list of keys, ignoring blanks.
// Several keys let you rotate them without downtime.
func ParseAPIKeys(s string) []string {
	var keys []string
	for _, k := range strings.Split(s, ",") {
		if k = strings.TrimSpace(k); k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}
