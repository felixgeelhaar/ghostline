package main

import (
	"errors"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

func loadStaticSiteConfig(getenv func(string) string) (string, error) {
	directory := strings.TrimSpace(getenv("GHOSTLINE_STATIC_DIR"))
	production := strings.EqualFold(strings.TrimSpace(getenv("GHOSTLINE_ENV")), "production")
	if directory == "" {
		if production {
			return "", errors.New("GHOSTLINE_STATIC_DIR is required when GHOSTLINE_ENV=production")
		}
		return "", nil
	}
	if err := validateStaticSite(directory); err != nil {
		return "", err
	}
	return directory, nil
}

func registerStaticSite(router *gin.Engine, directory string) error {
	if strings.TrimSpace(directory) == "" {
		return nil
	}
	if err := validateStaticSite(directory); err != nil {
		return err
	}
	root := http.Dir(directory)
	files := http.FileServer(root)
	router.NoRoute(func(c *gin.Context) {
		requestPath := path.Clean("/" + c.Request.URL.Path)
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Status(http.StatusNotFound)
			return
		}
		if requestPath == "/api" || strings.HasPrefix(requestPath, "/api/") || requestPath == "/auth" || strings.HasPrefix(requestPath, "/auth/") {
			c.Status(http.StatusNotFound)
			return
		}

		filePath := strings.TrimPrefix(requestPath, "/")
		if filePath == "" {
			filePath = "index.html"
		}
		setStaticSecurityHeaders(c)
		file, err := root.Open(filePath)
		if err == nil {
			info, statErr := file.Stat()
			_ = file.Close()
			if statErr == nil && !info.IsDir() {
				if filePath == "index.html" {
					c.Header("Cache-Control", "no-cache")
				}
				files.ServeHTTP(c.Writer, c.Request)
				return
			}
		}
		if path.Ext(requestPath) != "" {
			c.Status(http.StatusNotFound)
			return
		}

		c.Header("Cache-Control", "no-cache")
		http.ServeFile(c.Writer, c.Request, filepath.Join(directory, "index.html"))
	})
	return nil
}

func setStaticSecurityHeaders(c *gin.Context) {
	c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; img-src 'self' data:; font-src 'self' data: https://fonts.gstatic.com; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
	c.Header("X-Frame-Options", "DENY")
	c.Header("X-Content-Type-Options", "nosniff")
}

func validateStaticSite(directory string) error {
	file, err := http.Dir(directory).Open("index.html")
	if err != nil {
		return fmt.Errorf("open Ghostline static entry point: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect Ghostline static entry point: %w", err)
	}
	if info.IsDir() {
		return errors.New("Ghostline static entry point must be a file")
	}
	return nil
}
