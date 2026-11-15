package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRegisterStaticSiteServesBuildAndKeepsAPIAndAuthPathsOutOfFallback(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<!doctype html><title>Ghostline</title>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "app.js"), []byte("window.ghostline = true"), 0o644); err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	if err := registerStaticSite(router, root); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path       string
		status     int
		contains   string
		notContain string
	}{
		{path: "/", status: http.StatusOK, contains: "Ghostline"},
		{path: "/assets/app.js", status: http.StatusOK, contains: "window.ghostline"},
		{path: "/deep/client/route", status: http.StatusOK, contains: "Ghostline"},
		{path: "/assets/missing.js", status: http.StatusNotFound, notContain: "Ghostline"},
		{path: "/api/not-found", status: http.StatusNotFound, notContain: "Ghostline"},
		{path: "/auth/not-found", status: http.StatusNotFound, notContain: "Ghostline"},
	} {
		t.Run(test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.status, response.Body.String())
			}
			if test.contains != "" && !strings.Contains(response.Body.String(), test.contains) {
				t.Fatalf("body does not contain %q: %s", test.contains, response.Body.String())
			}
			if test.notContain != "" && strings.Contains(response.Body.String(), test.notContain) {
				t.Fatalf("body unexpectedly contains %q: %s", test.notContain, response.Body.String())
			}
		})
	}
}

func TestRegisterStaticSiteRequiresBuiltIndex(t *testing.T) {
	if err := registerStaticSite(gin.New(), t.TempDir()); err == nil {
		t.Fatal("registerStaticSite() accepted a directory without index.html")
	}
}

func TestLoadStaticSiteConfigRequiresBundleInProduction(t *testing.T) {
	if _, err := loadStaticSiteConfig(func(key string) string {
		if key == "GHOSTLINE_ENV" {
			return "production"
		}
		return ""
	}); err == nil {
		t.Fatal("production accepted a missing static bundle")
	}

	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "index.html"), []byte("Ghostline"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadStaticSiteConfig(func(key string) string {
		switch key {
		case "GHOSTLINE_ENV":
			return "production"
		case "GHOSTLINE_STATIC_DIR":
			return directory
		default:
			return ""
		}
	})
	if err != nil || got != directory {
		t.Fatalf("loadStaticSiteConfig() = %q, %v; want %q", got, err, directory)
	}

	got, err = loadStaticSiteConfig(func(string) string { return "" })
	if err != nil || got != "" {
		t.Fatalf("local loadStaticSiteConfig() = %q, %v; want no static site", got, err)
	}
}
