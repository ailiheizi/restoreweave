package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
)

func TestHandlerWithStaticServesSnapshotAndPreservesAPI(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("INDEX"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.js"), []byte("ASSET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "styles.css"), []byte("body{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := HandlerWithStatic(func(_ context.Context, _ command.Envelope) command.Result {
		return command.Result{Status: command.StatusSucceeded}
	}, Options{}, root)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		method, path string
		status       int
		body         string
	}{
		{http.MethodGet, "/", http.StatusOK, "INDEX"}, {http.MethodHead, "/app.js", http.StatusOK, ""},
		{http.MethodGet, "/app.js", http.StatusOK, "ASSET"}, {http.MethodGet, "/styles.css", http.StatusOK, "body{}"}, {http.MethodGet, "/settings/profile", http.StatusOK, "INDEX"},
		{http.MethodGet, "/missing.js", http.StatusNotFound, ""}, {http.MethodPost, "/", http.StatusMethodNotAllowed, ""},
		{http.MethodGet, "/api/v1", http.StatusNotFound, "not found"}, {http.MethodGet, "/api/v1/", http.StatusNotFound, "endpoint not found"}, {http.MethodGet, "/api/v1/healthz", http.StatusOK, "\"status\":\"ok\""}, {http.MethodPost, "/api/v1/command", http.StatusBadRequest, "valid JSON"},
	}
	for _, test := range []struct {
		path, contentType string
		length            int
	}{{"/", "text/html; charset=utf-8", 5}, {"/app.js", "text/javascript; charset=utf-8", 5}, {"/styles.css", "text/css; charset=utf-8", 6}} {
		get := httptest.NewRecorder()
		handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "http://loopback"+test.path, nil))
		head := httptest.NewRecorder()
		handler.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "http://loopback"+test.path, nil))
		if get.Header().Get("Content-Type") != test.contentType || head.Header().Get("Content-Type") != test.contentType || head.Header().Get("Content-Length") != fmt.Sprint(test.length) || head.Body.Len() != 0 {
			t.Errorf("headers %s: GET=%v HEAD=%v body=%q", test.path, get.Header(), head.Header(), head.Body.String())
		}
	}
	for _, test := range tests {
		req := httptest.NewRequest(test.method, "http://loopback"+test.path, nil)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != test.status || (test.body != "" && !strings.Contains(res.Body.String(), test.body)) {
			t.Errorf("%s %s = %d %q, want %d containing %q", test.method, test.path, res.Code, res.Body.String(), test.status, test.body)
		}
	}
	for _, encoded := range []string{"/%2e%2e/index.html", "/..%2findex.html", "/%5cetc%5cpasswd", "/%00bad"} {
		req := httptest.NewRequest(http.MethodGet, "http://loopback"+encoded, nil)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusNotFound {
			t.Errorf("unsafe path %s = %d, want 404", encoded, res.Code)
		}
	}
}

func TestNewStaticHandlerRejectsUnsafeRootsAndAssets(t *testing.T) {
	if _, err := NewStaticHandler("relative"); err == nil {
		t.Fatal("relative root accepted")
	}
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := NewStaticHandler(missing); err == nil {
		t.Fatal("missing root accepted")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStaticHandler(file); err == nil {
		t.Fatal("file root accepted")
	}
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "index.html"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStaticHandler(link); err == nil {
		t.Fatal("symlink root accepted")
	}
	assetLink := filepath.Join(target, "app.js")
	if err := os.Symlink(filepath.Join(target, "index.html"), assetLink); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStaticHandler(target); err == nil {
		t.Fatal("symlink asset accepted")
	}
	noIndex := t.TempDir()
	if err := os.WriteFile(filepath.Join(noIndex, "app.js"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStaticHandler(noIndex); err == nil {
		t.Fatal("missing index accepted")
	}
}

func TestHandlerWithStaticDisabledMatchesAPI(t *testing.T) {
	handler, err := HandlerWithStatic(nil, Options{}, "")
	if err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "http://loopback/", nil))
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("disabled static root status = %d", res.Code)
	}
}
