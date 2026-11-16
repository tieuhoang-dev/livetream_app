package devstack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDotenvAndEnvironmentPrecedence(t *testing.T) {
	root := t.TempDir()
	err := os.WriteFile(filepath.Join(root, ".env"), []byte("REDIS_ADDR=127.0.0.1:16379\nREDIS_PASSWORD=file-password\nLIVEKIT_URL=wss://example.livekit.cloud\nLIVEKIT_API_KEY=file-key\nLIVEKIT_API_SECRET=file-secret\n"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"REDIS_ADDR", "LIVEKIT_URL", "LIVEKIT_API_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET", "DEV_COMPOSE_PROJECT"} {
		value, exists := os.LookupEnv(key)
		os.Unsetenv(key)
		t.Cleanup(func() {
			if exists {
				os.Setenv(key, value)
			} else {
				os.Unsetenv(key)
			}
		})
	}
	t.Setenv("REDIS_PASSWORD", "environment-password")
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if c.RedisAddr != "127.0.0.1:16379" || c.RedisPassword != "environment-password" || c.Env["LIVEKIT_API_KEY"] != "file-key" {
		t.Fatal("dotenv must load at runtime and preserve process environment precedence")
	}
	if c.Env["LIVEKIT_URL"] != "wss://example.livekit.cloud" || c.Env["LIVEKIT_API_SECRET"] != "file-secret" {
		t.Fatal("Cloud settings not loaded unchanged")
	}
}

func TestConfigPreservesCloudSettingsWithoutLocalDefaults(t *testing.T) {
	for _, input := range []map[string]string{
		{},
		{"LIVEKIT_URL": "wss://example.livekit.cloud/", "LIVEKIT_API_URL": "", "LIVEKIT_API_KEY": "configured-key", "LIVEKIT_API_SECRET": "configured-secret"},
		{"LIVEKIT_URL": "wss://example.livekit.cloud", "LIVEKIT_API_URL": "https://api.example.test", "LIVEKIT_API_KEY": "configured-key", "LIVEKIT_API_SECRET": "configured-secret"},
	} {
		c, err := FromEnv(".", input)
		if err != nil {
			t.Fatal(err)
		}
		if c.RedisAddr != "127.0.0.1:6379" {
			t.Fatal("Redis default missing")
		}
		c.composeEnv()
		for _, key := range []string{"LIVEKIT_URL", "LIVEKIT_API_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"} {
			if c.Env[key] != input[key] {
				t.Fatalf("%s overwritten by launcher", key)
			}
		}
		for _, key := range []string{"DEV_LIVEKIT_CONFIG", "DEV_LIVEKIT_PORT", "DEV_LIVEKIT_TCP_PORT", "DEV_LIVEKIT_UDP_PORT"} {
			if _, ok := c.Env[key]; ok {
				t.Fatalf("local LiveKit config generated: %s", key)
			}
		}
		if _, ok := input["REDIS_ADDR"]; ok {
			t.Fatal("input environment mutated")
		}
	}
}

func TestInvalidConfigDoesNotLeakValues(t *testing.T) {
	for _, input := range []map[string]string{
		{"REDIS_ADDR": "password-sensitive"},
		{"REDIS_ADDR": "127.0.0.1:70000"},
		{"DEV_COMPOSE_PROJECT": "Invalid Project"},
	} {
		_, err := FromEnv(".", input)
		if err == nil {
			t.Fatal("invalid configuration accepted")
		}
		if strings.Contains(err.Error(), "password-sensitive") {
			t.Fatal("configuration value leaked")
		}
	}
}
