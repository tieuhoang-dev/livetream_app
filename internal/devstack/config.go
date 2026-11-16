// Package devstack starts local Redis without taking over reachable services.
// LiveKit Cloud settings pass through unchanged to the host API.
package devstack

import (
	"errors"
	"fmt"
	"net"
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
	defaults := map[string]string{"REDIS_ADDR": "127.0.0.1:6379", "DEV_COMPOSE_PROJECT": "livemart-dev"}
	for key, value := range defaults {
		if env[key] == "" {
			env[key] = value
		}
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
	return &Config{
		Root: root, Env: env,
		RedisAddr: env["REDIS_ADDR"], RedisPassword: env["REDIS_PASSWORD"],
		Project: env["DEV_COMPOSE_PROJECT"],
	}, nil
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

func (c *Config) composeEnv() {
	_, port, _ := net.SplitHostPort(c.RedisAddr)
	c.Env["DEV_REDIS_PORT"] = port
}
