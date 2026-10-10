package static

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><title>todo</title>")},
		"assets/index-abc.js":  {Data: []byte("console.log('todo')")},
		"assets/index-abc.css": {Data: []byte("body{}")},
	}
}

func get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	Handler(testFS()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestServesIndexWithSecurityHeaders(t *testing.T) {
	rec := get(t, "/")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>todo</title>") {
		t.Fatalf("GET / = %d %q", rec.Code, rec.Body.String())
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "connect-src 'self'") || !strings.Contains(csp, "default-src 'self'") {
		t.Fatalf("Content-Security-Policy = %q", csp)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("index Cache-Control = %q, want no-cache", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
}

func TestHashedAssetsAreImmutable(t *testing.T) {
	rec := get(t, "/assets/index-abc.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET asset = %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("asset Cache-Control = %q, want immutable", got)
	}
}

func TestNoDirectoryListing(t *testing.T) {
	if rec := get(t, "/assets/"); rec.Code != http.StatusNotFound {
		t.Fatalf("GET /assets/ = %d %q, want 404", rec.Code, rec.Body.String())
	}
}

func TestMissingFile(t *testing.T) {
	if rec := get(t, "/nope.js"); rec.Code != http.StatusNotFound {
		t.Fatalf("GET /nope.js = %d, want 404", rec.Code)
	}
}

func TestHealthz(t *testing.T) {
	if rec := get(t, "/healthz"); rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz = %d", rec.Code)
	}
}
