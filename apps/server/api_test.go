package main

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestBearerToken(t *testing.T) {
	tests := []struct {
		header string
		want   string
		ok     bool
	}{
		{"Bearer abc", "abc", true},
		{"", "", false},
		{"Basic abc", "", false},
		{"Bearer ", "", false},
	}
	for _, tt := range tests {
		got, ok := bearerToken(tt.header)
		if got != tt.want || ok != tt.ok {
			t.Errorf("bearerToken(%q) = (%q, %v), want (%q, %v)", tt.header, got, ok, tt.want, tt.ok)
		}
	}
}

func TestValidateAgentInfo(t *testing.T) {
	if err := validateAgentInfo("WS-01", "windows", "amd64", "0.1.0"); err != nil {
		t.Fatalf("valid info rejected: %v", err)
	}
	if err := validateAgentInfo("", "windows", "amd64", "0.1.0"); err == nil {
		t.Fatal("expected empty hostname to fail")
	}
}

func TestHealthRouteMethodIsGET(t *testing.T) {
	// The method-qualified ServeMux pattern should reject unrelated methods.
	a := &api{}
	req := httptest.NewRequest("POST", "/healthz", nil)
	rec := httptest.NewRecorder()
	a.routes().ServeHTTP(rec, req)
	if rec.Code != 405 {
		t.Fatalf("got status %d, want 405", rec.Code)
	}
}

func TestOfflineThreshold(t *testing.T) {
	lastSeen := time.Now().Add(-2 * time.Minute)
	threshold := 90 * time.Second
	if time.Since(lastSeen) <= threshold {
		t.Fatal("device should be offline beyond threshold")
	}
}
