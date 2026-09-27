package config

import (
	"strings"
	"testing"
)

func validTestConfig() *Config {
	return &Config{
		DBPath:      ":memory:",
		QBHost:      "http://localhost:8080",
		ServerPort:  7892,
		JWTSecret:   "0123456789abcdef0123456789abcdef",
		JWTUsername: "admin",
		JWTPassword: "strong-password",
		AuthEnabled: true,
	}
}

func TestValidateAuthEnabledRejectsDefaultSecret(t *testing.T) {
	cfg := validTestConfig()
	cfg.JWTSecret = "your-secret-key-change-in-production"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected default JWT_SECRET to fail validation")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("expected JWT_SECRET validation error, got %v", err)
	}
}

func TestValidateAuthEnabledRejectsDefaultPassword(t *testing.T) {
	cfg := validTestConfig()
	cfg.JWTPassword = "admin"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected default JWT_PASSWORD to fail validation")
	}
	if !strings.Contains(err.Error(), "JWT_PASSWORD") {
		t.Fatalf("expected JWT_PASSWORD validation error, got %v", err)
	}
}

func TestValidateAuthDisabledAllowsLocalDefaults(t *testing.T) {
	cfg := validTestConfig()
	cfg.AuthEnabled = false
	cfg.JWTSecret = "your-secret-key-change-in-production"
	cfg.JWTPassword = "admin"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected local no-auth defaults to validate, got %v", err)
	}
}

func TestValidateHomeNotificationRequiresTokenWhenEnabled(t *testing.T) {
	cfg := validTestConfig()
	cfg.AuthEnabled = false
	cfg.HomeNotificationEnabled = true
	cfg.HomeNotificationURL = "https://home.example"

	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "HOME_NOTIFICATION_TOKEN") {
		t.Fatalf("expected home token validation error, got %v", err)
	}
}

func TestValidateHomeNotificationRequiresURLWhenEnabled(t *testing.T) {
	cfg := validTestConfig()
	cfg.AuthEnabled = false
	cfg.HomeNotificationEnabled = true
	cfg.HomeNotificationToken = "test-token"

	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "HOME_NOTIFICATION_URL") {
		t.Fatalf("expected home URL validation error, got %v", err)
	}
}
