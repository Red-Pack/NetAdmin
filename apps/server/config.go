package main

import (
	"fmt"
	"os"
	"time"
)

type config struct {
	HTTPAddr            string
	DatabaseURL         string
	EnrollmentToken     string
	OfflineAfter        time.Duration
}

func loadConfig() (config, error) {
	c := config{
		HTTPAddr:        env("HTTP_ADDR", ":8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		EnrollmentToken: os.Getenv("AGENT_ENROLLMENT_TOKEN"),
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	if len(c.EnrollmentToken) < 32 {
		return c, fmt.Errorf("AGENT_ENROLLMENT_TOKEN must contain at least 32 characters")
	}
	raw := env("OFFLINE_AFTER", "90s")
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return c, fmt.Errorf("OFFLINE_AFTER must be a positive duration")
	}
	c.OfflineAfter = d
	return c, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
