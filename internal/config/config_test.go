package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func chdirTemp(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Errorf("failed to restore working dir: %v", err)
		}
	})
}

func TestDefaultGeneratesSecureCredentials(t *testing.T) {
	chdirTemp(t)
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
	chdirTemp(t)
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
	chdirTemp(t)
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
	chdirTemp(t)
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

func TestDefaultReusesPersistedPassword(t *testing.T) {
	chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(".pg", "data"), 0o700); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	first := Default()
	if first.DBPass == "" {
		t.Fatalf("expected password to be generated")
	}
	second := Default()
	if first.DBPass != second.DBPass {
		t.Fatalf("expected password to remain stable across restarts")
	}
}
