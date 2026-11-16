// Package devstack starts local Redis/LiveKit dependencies without taking over
// reachable services or changing the host's MySQL/MongoDB configuration.
package devstack

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Root          string
	Env           map[string]string
	RedisAddr     string
	RedisPassword string
	LiveURL       *url.URL
	LiveAPI       *url.URL
	Key           string
	Secret        string
	TCPPort       int
	UDPPort       int
	Project       string
}

func Load(root string) (*Config, error) {
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	values, err := godotenv.Read(filepath.Join(root, ".env"))
	if err != nil {
		return nil, errors.New("khong doc duoc .env; copy .env.example thanh .env va dien cau hinh MySQL/MongoDB truoc")
	}
	for key, value := range values {
		if _, exists := env[key]; !exists {
			env[key] = value
		}
	}
	return FromEnv(root, env)
}

func FromEnv(root string, input map[string]string) (*Config, error) {
	env := make(map[string]string, len(input)+8)
	for key, value := range input {
		env[key] = value
	}
	defaults := map[string]string{"REDIS_ADDR": "127.0.0.1:6379", "LIVEKIT_URL": "ws://127.0.0.1:7880", "DEV_COMPOSE_PROJECT": "livemart-dev"}
	for key, value := range defaults {
		if env[key] == "" {
			env[key] = value
		}
	}
	live, err := origin(env["LIVEKIT_URL"], "ws", "wss")
	if err != nil {
		return nil, errors.New("LIVEKIT_URL phai la origin ws:// hoac wss://, khong kem path/token")
	}
	if env["LIVEKIT_API_URL"] == "" {
		api := *live
		api.Scheme = "http" + strings.TrimPrefix(api.Scheme, "ws")
		env["LIVEKIT_API_URL"] = api.String()
	}
	api, err := origin(env["LIVEKIT_API_URL"], "http", "https")
	if err != nil {
		return nil, errors.New("LIVEKIT_API_URL phai la origin http:// hoac https://")
	}
	if env["LIVEKIT_API_KEY"] == "" && env["LIVEKIT_API_SECRET"] == "" && loopback(api.Hostname()) {
		env["LIVEKIT_API_KEY"], env["LIVEKIT_API_SECRET"] = "devkey", "secret"
	}
	if env["LIVEKIT_API_KEY"] == "" || env["LIVEKIT_API_SECRET"] == "" {
		return nil, errors.New("dien ca LIVEKIT_API_KEY va LIVEKIT_API_SECRET trong .env")
	}
	host, port, err := net.SplitHostPort(env["REDIS_ADDR"])
	if err != nil || host == "" {
		return nil, errors.New("REDIS_ADDR phai co dang host:port")
	}
	if _, err = validPort(port); err != nil {
		return nil, fmt.Errorf("REDIS_ADDR: %w", err)
	}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`).MatchString(env["DEV_COMPOSE_PROJECT"]) {
		return nil, errors.New("DEV_COMPOSE_PROJECT chi gom chu thuong, so, dau - hoac _")
	}
	tcp, err := portSetting(env, "DEV_LIVEKIT_TCP_PORT", 7881)
	if err != nil {
		return nil, err
	}
	udp, err := portSetting(env, "DEV_LIVEKIT_UDP_PORT", 7882)
	if err != nil {
		return nil, err
	}
	return &Config{
		Root: root, Env: env,
		RedisAddr: env["REDIS_ADDR"], RedisPassword: env["REDIS_PASSWORD"],
		LiveURL: live, LiveAPI: api,
		Key: env["LIVEKIT_API_KEY"], Secret: env["LIVEKIT_API_SECRET"],
		TCPPort: tcp, UDPPort: udp, Project: env["DEV_COMPOSE_PROJECT"],
	}, nil
}

func origin(value string, plain string, secure string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(value, "/"))
	if err != nil || (u.Scheme != plain && u.Scheme != secure) || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("invalid origin")
	}
	if u.Port() != "" {
		if _, err := validPort(u.Port()); err != nil {
			return nil, err
		}
	}
	return u, nil
}

func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validPort(value string) (int, error) {
	p, err := strconv.Atoi(value)
	if err != nil || p < 1 || p > 65535 {
		return 0, errors.New("port phai tu 1 den 65535")
	}
	return p, nil
}

func portSetting(env map[string]string, key string, fallback int) (int, error) {
	value := env[key]
	if value == "" {
		return fallback, nil
	}
	p, err := validPort(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return p, nil
}

func (c *Config) Environ() []string {
	keys := make([]string, 0, len(c.Env))
	for key := range c.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, key+"="+c.Env[key])
	}
	return values
}

func (c *Config) composeEnv() error {
	_, port, _ := net.SplitHostPort(c.RedisAddr)
	c.Env["DEV_REDIS_PORT"] = port
	c.Env["DEV_LIVEKIT_PORT"] = c.LiveAPI.Port()
	if c.Env["DEV_LIVEKIT_PORT"] == "" {
		c.Env["DEV_LIVEKIT_PORT"] = "7880"
	}
	c.Env["DEV_LIVEKIT_TCP_PORT"], c.Env["DEV_LIVEKIT_UDP_PORT"] = strconv.Itoa(c.TCPPort), strconv.Itoa(c.UDPPort)
	body, err := json.Marshal(map[string]interface{}{
		"port": 7880,
		"keys": map[string]string{c.Key: c.Secret},
		"rtc": map[string]interface{}{
			"tcp_port": c.TCPPort, "udp_port": c.UDPPort, "use_external_ip": false,
		},
		"logging": map[string]string{"level": "info"},
	})
	if err == nil {
		c.Env["DEV_LIVEKIT_CONFIG"] = string(body)
	}
	return err
}
