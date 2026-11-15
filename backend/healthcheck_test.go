package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckHealth(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{name: "healthy", statusCode: http.StatusOK},
		{name: "unhealthy", statusCode: http.StatusServiceUnavailable, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			err := checkHealth(server.Client(), server.URL)
			if (err != nil) != tt.wantErr {
				t.Fatalf("checkHealth() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
