package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

// LogPanics is a development middleware that recovers panics and logs a full
// stack trace to the server logger. Useful to capture the cause of intermittent
// 500 errors during local debugging. Intended for local/dev use only.
func LogPanics() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					slog.Error("Panic recovered in request", "panic", rec, "stack", string(debug.Stack()), "path", r.URL.Path, "method", r.Method)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
