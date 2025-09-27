package config

import (
	"os"
	"strings"
	"testing"
)

func TestDefaultGeneratesSecureCredentials(t *testing.T) {
	cfg := Default()
	if cfg.DBUser == "postgres" {
		t.Fatalf("expected default db user to change from postgres")
	}
	if len(cfg.DBPass) < 12 {
		t.Fatalf("expected secure password length, got %d", len(cfg.DBPass))
	}
	if err := validatePassword(cfg.DBPass); err != nil {
		t.Fatalf("default password not secure: %v", err)
	}
}

func TestNormalizeRespectsEnvOverrides(t *testing.T) {
	t.Setenv("WATCHER_DB_ENABLE", "false")
	t.Setenv("WATCHER_DB_EXPOSE", "1")
	t.Setenv("WATCHER_DB_SSL", "no")
	t.Setenv("WATCHER_DB_LISTEN", "0.0.0.0")
	t.Setenv("WATCHER_DB_USER", "tester")
	t.Setenv("WATCHER_DB_PASS", "Supersicher123")
	cfg := Default()
	cfg.Normalize()
	if cfg.DBEnable {
		t.Fatalf("expected DBEnable to be false")
	}
	if !cfg.DBExpose {
		t.Fatalf("expected DBExpose to be true")
	}
	if cfg.DBSSL {
		t.Fatalf("expected DBSSL to be false")
	}
	if cfg.DBListen != "0.0.0.0" {
		t.Fatalf("unexpected listen address: %s", cfg.DBListen)
	}
	if cfg.DBUser != "tester" {
		t.Fatalf("unexpected user: %s", cfg.DBUser)
	}
	if cfg.DBPass != "Supersicher123" {
		t.Fatalf("unexpected password override")
	}
}

func TestValidateEnforcesPasswordRules(t *testing.T) {
	cfg := Default()
	cfg.DBPass = "weak"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "db_pass") {
		t.Fatalf("expected password validation error, got %v", err)
	}

	cfg.DBPass = "LangesPasswort1"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid configuration, got %v", err)
	}
}

func TestValidateRequiresListenForExpose(t *testing.T) {
	cfg := Default()
	cfg.DBExpose = true
	cfg.DBListen = ""
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected expose validation error")
	}

	cfg.DBListen = "0.0.0.0"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid configuration, got %v", err)
	}
}

func TestParseBoolEnv(t *testing.T) {
	key := "WATCHER_TEST_BOOL"
	t.Cleanup(func() { os.Unsetenv(key) })

	if _, ok := parseBoolEnv(key); ok {
		t.Fatalf("expected no value present")
	}

	cases := map[string]bool{
		"true":  true,
		"FALSE": false,
		"On":    true,
		"0":     false,
	}
	for val, expect := range cases {
		os.Setenv(key, val)
		actual, ok := parseBoolEnv(key)
		if !ok {
			t.Fatalf("expected value %s to be parsed", val)
		}
		if actual != expect {
			t.Fatalf("unexpected bool parse: %s -> %v", val, actual)
		}
	}
}
