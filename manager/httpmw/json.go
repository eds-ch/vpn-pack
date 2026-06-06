package httpmw

import (
	"net/http"
	"strings"
)

func RequireJSON(maxBytes int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodDelete {
				next.ServeHTTP(w, r)
				return
			}
			ct := r.Header.Get("Content-Type")
			if i := strings.IndexByte(ct, ';'); i >= 0 {
				ct = ct[:i]
			}
			if strings.TrimSpace(ct) != "application/json" {
				http.Error(w, "expect application/json", http.StatusUnsupportedMediaType)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}
