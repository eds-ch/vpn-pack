package httpmw

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

func Recover() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					slog.Error("http handler panic",
						"panic", rec,
						"path", r.URL.Path,
						"method", r.Method,
						"stack", string(debug.Stack()))
					http.Error(w, "internal error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
