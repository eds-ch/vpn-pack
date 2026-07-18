package httpmw

import "net/http"

// contentSecurityPolicy is served on every response. CSP is the one
// security header nginx does not strip via proxy_hide_header, so the
// manager sets it at the app layer. 'unsafe-inline' for style-src is
// intentional: the SPA ships inline styles and would fail to render
// without it. frame-ancestors 'none' blocks clickjacking.
const contentSecurityPolicy = "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'"

// SecurityHeaders sets response security headers (currently CSP) on every
// response, including SPA assets and API responses. The header is written
// before the wrapped handler runs so it is present regardless of the status
// code the handler eventually writes.
func SecurityHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
			next.ServeHTTP(w, r)
		})
	}
}
