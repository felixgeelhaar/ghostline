// Command signin-monitor is Ghostline's scheduled synthetic check of the
// signed-out journey on a deployment. See docs/synthetic-monitoring.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	var cfg Config
	flag.StringVar(&cfg.Base, "base", "https://ghostline.felixgeelhaar.de", "deployment origin")
	flag.StringVar(&cfg.GitHub, "github", "https://github.com", "GitHub origin")
	flag.StringVar(&cfg.ExpectVersion, "expect-version", "", "release the deploy manifest pins, e.g. v0.1.0-rc.12")
	flag.StringVar(&cfg.ExpectApp, "expect-app", "Ghostline", "OAuth app name GitHub shows on its sign-in page")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fmt.Printf("checking %s at %s (release %s)\n", cfg.Base, time.Now().UTC().Format(time.RFC3339), cfg.ExpectVersion)
	steps, err := Run(ctx, http.DefaultTransport, cfg)
	for _, step := range steps {
		fmt.Printf("PASS %s: %s\n", step.Name, step.Detail)
	}
	if err != nil {
		fmt.Printf("FAIL %v\n", err)
		os.Exit(1)
	}
	fmt.Println("all checks passed")
}
