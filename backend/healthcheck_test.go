package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContainerHealthEndpoint(t *testing.T) {
	tests := []struct {
		name string
		host string
		port string
		want string
	}{
		{name: "default host and port", want: "http://127.0.0.1:8080/api/health"},
		{name: "custom bind", host: "192.0.2.10", port: "9090", want: "http://192.0.2.10:9090/api/health"},
		{name: "wildcard IPv4 bind", host: "0.0.0.0", port: "8080", want: "http://127.0.0.1:8080/api/health"},
		{name: "wildcard IPv6 bind", host: "::", port: "8080", want: "http://[::1]:8080/api/health"},
		{name: "concrete IPv6 bind", host: "2001:db8::1", port: "9090", want: "http://[2001:db8::1]:9090/api/health"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containerHealthEndpoint(tt.host, tt.port); got != tt.want {
				t.Fatalf("containerHealthEndpoint(%q, %q) = %q, want %q", tt.host, tt.port, got, tt.want)
			}
		})
	}
}

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
