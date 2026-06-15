package modules

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if g.Header().Get("Content-Type") == "" {
		g.Header().Set("Content-Type", http.DetectContentType(b))
	}
	return g.Writer.Write(b)
}

func GzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		if strings.HasPrefix(r.URL.Path, "/ws/") {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")

		gz := gzip.NewWriter(w)
		defer gz.Close()

		gw := &gzipResponseWriter{Writer: gz, ResponseWriter: w}
		next.ServeHTTP(gw, r)
	})
}

func CacheControlMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css") ||
			strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".jpg") ||
			strings.HasSuffix(path, ".svg") || strings.HasSuffix(path, ".ico") {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		} else if strings.HasSuffix(path, ".json") {
			w.Header().Set("Cache-Control", "public, max-age=300")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, `{"status":"ok","service":"zhaozhou-bridge-monitor"}`)
}
