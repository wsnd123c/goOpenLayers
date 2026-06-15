package server

import "net/http"

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
	if r.URL == nil || r.URL.Query().Get("status") != "editing" {
		return
	}

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}
