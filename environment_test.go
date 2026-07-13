package shost_test

import (
	"testing"

	"github.com/dvislobokov/shost"
)

func TestEnvironmentFromEnv(t *testing.T) {
	t.Setenv("APP_ENVIRONMENT", "Development")
	if env := shost.EnvironmentFromEnv(""); !env.IsDevelopment() {
		t.Fatalf("expected Development, got %v", env)
	}

	t.Setenv("MY_ENV", "staging") // case-insensitive
	if env := shost.EnvironmentFromEnv("MY_ENV"); !env.IsStaging() {
		t.Fatalf("expected Staging, got %v", env)
	}

	t.Setenv("UNSET_ENV", "")
	if env := shost.EnvironmentFromEnv("UNSET_ENV"); !env.IsProduction() {
		t.Fatalf("expected Production default, got %v", env)
	}
}

func TestHostEnvironment(t *testing.T) {
	h := shost.New().WithEnvironment(shost.Development).MustBuild()
	if !h.Environment().IsDevelopment() {
		t.Fatalf("expected Development, got %v", h.Environment())
	}

	h = shost.New().MustBuild()
	if !h.Environment().IsProduction() {
		t.Fatalf("expected Production default, got %v", h.Environment())
	}
}

func TestBuildRejectsEmptyEnvironment(t *testing.T) {
	_, err := shost.New().WithEnvironment("").Build()
	if err == nil {
		t.Fatal("expected error for empty environment")
	}
}
