package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

func runHealthcheckCommand(args []string) error {
	var target string
	var timeout time.Duration

	flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&target, "url", "", "health endpoint URL")
	flags.DurationVar(&timeout, "timeout", 0, "healthcheck timeout")

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected healthcheck arguments: %v", flags.Args())
	}
	if target == "" {
		return fmt.Errorf("healthcheck requires --url")
	}
	if timeout <= 0 {
		return fmt.Errorf("healthcheck requires a positive --timeout")
	}
	if err := validateHealthcheckURL(target); err != nil {
		return err
	}

	return checkHTTPHealth(context.Background(), target, timeout)
}

func validateHealthcheckURL(target string) error {
	parsed, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("invalid healthcheck URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("healthcheck URL must use http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("healthcheck URL must include a host")
	}
	return nil
}

func checkHTTPHealth(ctx context.Context, target string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("build healthcheck request: %w", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("healthcheck request failed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("healthcheck returned status %d", response.StatusCode)
	}
	return nil
}
