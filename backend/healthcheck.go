package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func joinHostPort(host, port string) string {
	host = strings.TrimSpace(host)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	return net.JoinHostPort(host, port)
}

func containerHealthEndpoint(host, port string) string {
	if strings.TrimSpace(port) == "" {
		port = "8080"
	}
	host = strings.TrimSpace(host)
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return "http://" + joinHostPort(host, port) + "/api/health"
}

func checkHealth(client *http.Client, endpoint string) error {
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request health endpoint: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned %s", response.Status)
	}
	return nil
}

func runContainerHealthcheck() error {
	client := &http.Client{Timeout: 2 * time.Second}
	return checkHealth(client, containerHealthEndpoint(os.Getenv("HOST"), os.Getenv("PORT")))
}
