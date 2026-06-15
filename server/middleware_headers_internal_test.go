package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetEditingTileNoStoreHeaders(t *testing.T) {
	tests := map[string]struct {
		rawURL        string
		expectNoStore bool
		expectPragma  bool
		expectExpires bool
	}{
		"editing disables cache": {
			rawURL:        "/maps/vector/schema/table/10/1/1.pbf?status=editing",
			expectNoStore: true,
			expectPragma:  true,
			expectExpires: true,
		},
		"deleted does not change cache headers": {
			rawURL: "/maps/vector/schema/table/10/1/1.pbf?status=deleted",
		},
		"missing status does not change cache headers": {
			rawURL: "/maps/vector/schema/table/10/1/1.pbf",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.rawURL, nil)
			rec := httptest.NewRecorder()

			setEditingTileNoStoreHeaders(rec, req)

			if got := rec.Header().Get("Cache-Control"); (got != "") != tc.expectNoStore {
				t.Fatalf("Cache-Control header presence mismatch: %q", got)
			}
			if got := rec.Header().Get("Pragma"); (got != "") != tc.expectPragma {
				t.Fatalf("Pragma header presence mismatch: %q", got)
			}
			if got := rec.Header().Get("Expires"); (got != "") != tc.expectExpires {
				t.Fatalf("Expires header presence mismatch: %q", got)
			}
		})
	}
}
