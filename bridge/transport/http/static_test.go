package httptransport

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"guiforcores/bridge/platform"
	"guiforcores/bridge/storage"
)

func TestFrontendHandlerStaticLookup(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		rolling      bool
		distFS       fstest.MapFS
		setupRolling func(t *testing.T, baseDir string)
		wantStatus   int
		wantBody     string
	}{
		{
			name:       "root returns embedded directory index",
			path:       "/",
			distFS:     frontendTestFS(),
			wantStatus: http.StatusOK,
			wantBody:   "embedded-root",
		},
		{
			name:       "rolling root directory index wins",
			path:       "/",
			rolling:    true,
			distFS:     frontendTestFS(),
			wantStatus: http.StatusOK,
			wantBody:   "rolling-root",
			setupRolling: func(t *testing.T, baseDir string) {
				writeRollingFile(t, baseDir, "index.html", "rolling-root")
			},
		},
		{
			name:       "rolling file wins",
			path:       "/assets/app.js",
			rolling:    true,
			distFS:     frontendTestFS(),
			wantStatus: http.StatusOK,
			wantBody:   "rolling-js",
			setupRolling: func(t *testing.T, baseDir string) {
				writeRollingFile(t, baseDir, "assets/app.js", "rolling-js")
			},
		},
		{
			name:       "rolling directory index wins",
			path:       "/docs",
			rolling:    true,
			distFS:     frontendTestFS(),
			wantStatus: http.StatusOK,
			wantBody:   "rolling-docs",
			setupRolling: func(t *testing.T, baseDir string) {
				writeRollingFile(t, baseDir, "docs/index.html", "rolling-docs")
			},
		},
		{
			name:       "rolling directory without index falls back to embedded same path",
			path:       "/docs",
			rolling:    true,
			distFS:     frontendTestFS(),
			wantStatus: http.StatusOK,
			wantBody:   "embedded-docs",
			setupRolling: func(t *testing.T, baseDir string) {
				writeRollingFile(t, baseDir, "docs/other.txt", "rolling-other")
			},
		},
		{
			name:       "embedded directory index is returned",
			path:       "/docs",
			distFS:     frontendTestFS(),
			wantStatus: http.StatusOK,
			wantBody:   "embedded-docs",
		},
		{
			name:       "embedded directory without index is not listed or fallbacked",
			path:       "/empty",
			distFS:     frontendTestFS(),
			wantStatus: http.StatusNotFound,
			wantBody:   "404 page not found\n",
		},
		{
			name:       "missing path returns not found",
			path:       "/profiles",
			distFS:     frontendTestFS(),
			wantStatus: http.StatusNotFound,
			wantBody:   "404 page not found\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := t.TempDir()
			if tt.setupRolling != nil {
				tt.setupRolling(t, baseDir)
			}
			server := &Server{options: Options{
				Platform: platform.NewService(storage.NewPaths(baseDir), nil, platform.Environment{}),
				RollingRelease: func() bool {
					return tt.rolling
				},
			}}
			handler := server.buildFrontendHandler(tt.distFS)

			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if response.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, response.Code)
			}
			if response.Body.String() != tt.wantBody {
				t.Fatalf("expected body %q, got %q", tt.wantBody, response.Body.String())
			}
		})
	}
}

func frontendTestFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html": {
			Data: []byte("embedded-root"),
		},
		"assets": {
			Mode: os.ModeDir,
		},
		"assets/app.js": {
			Data: []byte("embedded-js"),
		},
		"docs": {
			Mode: os.ModeDir,
		},
		"docs/index.html": {
			Data: []byte("embedded-docs"),
		},
		"empty": {
			Mode: os.ModeDir,
		},
		"empty/other.txt": {
			Data: []byte("embedded-other"),
		},
	}
}

func writeRollingFile(t *testing.T, baseDir, name, content string) {
	t.Helper()

	filePath := filepath.Join(baseDir, "data", "rolling-release", filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
