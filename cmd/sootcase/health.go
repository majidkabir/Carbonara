package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"time"
)

// runHealth checks a running server's /healthz, for container healthchecks
// in images without curl. Exit status 0 means healthy.
func runHealth(args []string) error {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	addr := fs.String("addr", envOr("ADDR", ":8080"), "address the server listens on")
	_ = fs.Parse(args)
	host, port, err := net.SplitHostPort(*addr)
	if err != nil {
		return fmt.Errorf("health: bad address %q: %w", *addr, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return fmt.Errorf("health: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health: status %d", resp.StatusCode)
	}
	return nil
}
