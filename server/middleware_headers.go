package server

import (
	"net/http"
	"strings"
)

// HeadersHandler is middleware for adding user defined response headers
func HeadersHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// set default and user defined headers
		setHeaders(w)
		// move on
		next.ServeHTTP(w, r)
		return
	})
}

func setEditingTileNoStoreHeaders(w http.ResponseWriter, r *http.Request) {
	if !hasEditingStatus(r) {
		return
	}

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func hasEditingStatus(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return false
	}
	if r.Form != nil {
		return r.Form.Get("status") == "editing"
	}

	rawQuery := r.URL.RawQuery
	for rawQuery != "" {
		part := rawQuery
		if idx := strings.IndexByte(rawQuery, '&'); idx >= 0 {
			part = rawQuery[:idx]
			rawQuery = rawQuery[idx+1:]
		} else {
			rawQuery = ""
		}
		if part == "status=editing" {
			return true
		}
	}
	return false
}
