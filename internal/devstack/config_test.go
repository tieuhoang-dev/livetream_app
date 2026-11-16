package devstack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDotenvAndEnvironmentPrecedence(t *testing.T) {
	root := t.TempDir()
	err := os.WriteFile(filepath.Join(root, ".env"), []byte("REDIS_ADDR=127.0.0.1:16379\nREDIS_PASSWORD=file-password\nLIVEKIT_URL=ws://localhost:17880\nLIVEKIT_API_KEY=file-key\nLIVEKIT_API_SECRET=file-secret\n"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"REDIS_ADDR", "LIVEKIT_URL", "LIVEKIT_API_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET", "DEV_LIVEKIT_TCP_PORT", "DEV_LIVEKIT_UDP_PORT", "DEV_COMPOSE_PROJECT"} {
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
	if c.RedisAddr != "127.0.0.1:16379" || c.RedisPassword != "environment-password" || c.Key != "file-key" {
		t.Fatal("dotenv must load at runtime and preserve process environment precedence")
	}
	if c.LiveAPI.String() != "http://localhost:17880" {
		t.Fatal("wrong derived room API origin")
	}
}

func TestConfigDefaultsAndPublicURL(t *testing.T) {
	input := map[string]string{}
	c, err := FromEnv(".", input)
	if err != nil {
		t.Fatal(err)
	}
	if c.RedisAddr != "127.0.0.1:6379" || c.Key != "devkey" || c.Secret != "secret" {
		t.Fatal("local development defaults missing")
	}
	if len(input) != 0 {
		t.Fatal("input environment mutated")
	}
	c, err = FromEnv(".", map[string]string{
		"LIVEKIT_URL": "wss://video.example.test/", "LIVEKIT_API_URL": "http://localhost:17880/",
		"LIVEKIT_API_KEY": "configured-key", "LIVEKIT_API_SECRET": "configured-secret",
		"DEV_LIVEKIT_TCP_PORT": "17881", "DEV_LIVEKIT_UDP_PORT": "17882",
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.LiveURL.String() != "wss://video.example.test" || c.LiveAPI.String() != "http://localhost:17880" {
		t.Fatal("public/internal origins not preserved")
	}
	if err := c.composeEnv(); err != nil {
		t.Fatal(err)
	}
	var generated struct {
		Keys map[string]string `json:"keys"`
		RTC  struct {
			TCP int `json:"tcp_port"`
			UDP int `json:"udp_port"`
		} `json:"rtc"`
	}
	if err := json.Unmarshal([]byte(c.Env["DEV_LIVEKIT_CONFIG"]), &generated); err != nil {
		t.Fatal(err)
	}
	if generated.Keys[c.Key] != c.Secret || generated.RTC.TCP != 17881 || generated.RTC.UDP != 17882 {
		t.Fatal("managed config does not match existing credentials/ports")
	}
}

func TestInvalidConfigDoesNotLeakValues(t *testing.T) {
	for _, input := range []map[string]string{
		{"REDIS_ADDR": "password-sensitive"},
		{"REDIS_ADDR": "127.0.0.1:70000"},
		{"LIVEKIT_URL": "https://example.test"},
		{"LIVEKIT_URL": "ws://user:password-sensitive@localhost:7880"},
		{"LIVEKIT_URL": "ws://localhost:7880/?token=password-sensitive"},
		{"LIVEKIT_URL": "ws://localhost:7880?"},
		{"LIVEKIT_API_KEY": "password-sensitive"},
		{"LIVEKIT_URL": "wss://example.test"},
		{"DEV_LIVEKIT_UDP_PORT": "-1"},
		{"DEV_COMPOSE_PROJECT": "Invalid Project"},
	} {
		_, err := FromEnv(".", input)
		if err == nil {
			t.Fatalf("invalid configuration accepted: keys %v", len(input))
		}
		if strings.Contains(err.Error(), "password-sensitive") {
			t.Fatal("configuration value leaked")
		}
	}
}
