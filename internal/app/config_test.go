package app

import (
	"os"
	"strings"
	"testing"
)

func startupEnvironment(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv("DATABASE_URL", "root:test@tcp(localhost:3306)/test")
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("JWT_SECRET", "startup-test-only")
	t.Setenv("PORT", "")
}
func TestSettingsWithoutDotenv(t *testing.T) {
	startupEnvironment(t)
	cfg, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.address != ":8080" {
		t.Fatal(cfg.address)
	}
	t.Setenv("PORT", "8089")
	if err := os.WriteFile(".env", []byte("PORT=9999\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = loadSettings()
	if err != nil || cfg.address != ":8089" {
		t.Fatal(".env overwrote the environment")
	}
}
func TestInvalidStartupSettings(t *testing.T) {
	startupEnvironment(t)
	for _, port := range []string{"0", "-1", "65536", "bad"} {
		t.Setenv("PORT", port)
		if _, err := loadSettings(); err == nil {
			t.Fatal("invalid port accepted", port)
		}
	}
	t.Setenv("PORT", "")
	t.Setenv("JWT_SECRET", "")
	if _, err := loadSettings(); err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatal("missing required setting accepted")
	}
}
