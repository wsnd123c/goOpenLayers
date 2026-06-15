package server

import (
	"net/http"
	"time"

	"github.com/go-spatial/tegola/internal/log"
)

// AccessLogHandler logs a concise line for each handled request.
func AccessLogHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &accessLogResponseWriter{
			ResponseWriter: w,
			status:         http.StatusOK,
		}

		next.ServeHTTP(lw, r)

		log.Infof(
			"%s %s status=%d bytes=%d dur=%s",
			r.Method,
			requestURI(r),
			lw.status,
			lw.bytes,
			time.Since(start).Round(time.Microsecond),
		)
	})
}

type accessLogResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *accessLogResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *accessLogResponseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func requestURI(r *http.Request) string {
	if r.URL == nil {
		return r.RequestURI
	}
	if r.URL.RawQuery == "" {
		return r.URL.Path
	}
	return r.URL.Path + "?" + r.URL.RawQuery
}
