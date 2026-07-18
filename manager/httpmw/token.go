package httpmw

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"sync"
)

const TokenHeader = "X-VpnPack-Token"

// Token enforces a per-install shared secret that the trusted nginx
// front-end injects via the X-VpnPack-Token header. It is a
// defense-in-depth factor layered AFTER PeerUIDAuth: it raises the bar
// for anything that can connect(2) to the manager socket as an allowed
// uid but that did not arrive through nginx (M1 — e.g. a process that
// gains the nginx group's socket access without nginx's request path).
//
// SECURITY CAVEAT (do not overstate the guarantee): the secret is placed
// into the nginx config, which the nginx worker itself reads. Against RCE
// *inside the nginx worker* this is therefore defense-in-depth, not a
// hard boundary — such an attacker can read the same secret and forge the
// header. PeerUIDAuth already restricts socket connectors to {root,
// nginx}, and both of those can read the secret; the marginal value of
// this factor is against a connector that has the socket's group access
// but NOT nginx's config-read path.
//
// Enforcement is tied to the presence of the secret. An empty secret
// DISABLES the factor (fail-open) with a loud one-time warning. This is
// deliberate: it avoids a self-DoS during a partial upgrade (new binary,
// secret/nginx-config not yet provisioned) and keeps dev/MOCK working
// where no secret exists. Once a non-empty secret is configured the
// factor is fail-closed — a request with a missing or mismatched header
// gets 403.
func Token(secret string) Middleware {
	expected := []byte(secret)
	enforce := len(expected) > 0
	var warnOnce sync.Once
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enforce {
				warnOnce.Do(func() {
					slog.Warn("token factor disabled: no X-VpnPack-Token secret configured; API gated only by peer-uid + nginx auth (expected in dev; on-device means the nginx-token file is missing)")
				})
				next.ServeHTTP(w, r)
				return
			}
			got := r.Header.Get(TokenHeader)
			if got == "" || subtle.ConstantTimeCompare([]byte(got), expected) != 1 {
				http.Error(w, "forbidden: token factor", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SameOrigin rejects mutating (non-safe) requests that a browser has
// explicitly labelled cross-site via the Sec-Fetch-Site header. It is a
// cheap browser-provided signal layered on top of the CSRF double-submit
// token. Requests without the header (non-browser clients such as curl or
// the on-device healthcheck, and older browsers) pass through — the
// header is advisory and its absence is not proof of a cross-site origin.
func SameOrigin() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "cross-origin" {
				http.Error(w, "forbidden: cross-site request", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
