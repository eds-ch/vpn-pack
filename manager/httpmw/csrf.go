package httpmw

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"sync/atomic"
)

const (
	csrfCookie = "vp_csrf"
	csrfHeader = "X-Csrf-Token"
)

// csrfSecure controls the Secure attribute on the CSRF cookie. Production
// boot sets it to true (https via nginx); tests set false (plain http).
var csrfSecure atomic.Bool

func init() { csrfSecure.Store(true) }

// CSRFSetSecureForTests flips the Secure flag for the duration of a test.
// MUST NOT be called from production code paths.
func CSRFSetSecureForTests(v bool) { csrfSecure.Store(v) }

// CSRF implements the double-submit cookie pattern. Safe-method requests
// (GET, HEAD, OPTIONS) are issued a random opaque token via cookie and pass
// through. Mutating requests must echo the same token in the
// X-Csrf-Token header; mismatch or absence is 403.
func CSRF() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			isSafe := r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions
			cookie, _ := r.Cookie(csrfCookie)
			if cookie == nil || cookie.Value == "" {
				if !isSafe {
					http.Error(w, "csrf check failed", http.StatusForbidden)
					return
				}
				tok := make([]byte, 32)
				if _, err := rand.Read(tok); err != nil {
					http.Error(w, "internal error", http.StatusInternalServerError)
					return
				}
				cookie = &http.Cookie{
					Name:     csrfCookie,
					Value:    hex.EncodeToString(tok),
					Path:     "/",
					HttpOnly: false,
					SameSite: http.SameSiteStrictMode,
					Secure:   csrfSecure.Load(),
				}
				http.SetCookie(w, cookie)
			}
			if isSafe {
				next.ServeHTTP(w, r)
				return
			}
			hdr := r.Header.Get(csrfHeader)
			if hdr == "" || subtle.ConstantTimeCompare([]byte(hdr), []byte(cookie.Value)) != 1 {
				http.Error(w, "csrf check failed", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
