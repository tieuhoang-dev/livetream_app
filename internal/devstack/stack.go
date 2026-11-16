package devstack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

type Runner func(context.Context, string, []string, []string) error

type Stack struct {
	Config *Config
	Output io.Writer
	Run    Runner
	owned  []string
}

func New(c *Config, output io.Writer) *Stack {
	stack := &Stack{Config: c, Output: output}
	stack.Run = func(ctx context.Context, program string, args []string, env []string) error {
		command := exec.CommandContext(ctx, program, args...)
		command.Dir = c.Root
		command.Env = env
		command.Stdout = output
		command.Stderr = output
		return command.Run()
	}
	return stack
}

func (s *Stack) compose(ctx context.Context, args ...string) error {
	base := []string{"compose", "--project-name", s.Config.Project, "--file", "compose.dev.yml"}
	return s.Run(ctx, "docker", append(base, args...), s.Config.Environ())
}

func (s *Stack) Start(ctx context.Context) error {
	c := s.Config
	// An occupied endpoint must authenticate as the expected service before reuse.
	// Never replace a remote service or a local process that merely owns the port.
	redisHost, redisPort, _ := net.SplitHostPort(c.RedisAddr)
	livePort := c.LiveAPI.Port()
	if livePort == "" {
		if c.LiveAPI.Scheme == "https" {
			livePort = "443"
		} else {
			livePort = "80"
		}
	}
	probes := []struct {
		name, addr string
		check      func(context.Context) error
	}{
		{"Redis", c.RedisAddr, s.checkRedis},
		{"LiveKit", net.JoinHostPort(c.LiveAPI.Hostname(), livePort), s.checkLiveKit},
	}
	var missing []string
	for _, probe := range probes {
		connection, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", probe.addr)
		if err == nil {
			connection.Close()
			if err := probe.check(ctx); err != nil {
				return fmt.Errorf("%s dang co dich vu nhung kiem tra xac thuc that bai; kiem tra .env (khong tu khoi dong/de len dich vu nay)", probe.name)
			}
			fmt.Fprintf(s.Output, "[dev] Dung %s dang chay tai %s\n", probe.name, probe.addr)
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if probe.name == "Redis" {
			if !loopback(redisHost) {
				return errors.New("Redis ben ngoai khong truy cap duoc; khong khoi dong Redis local thay the")
			}
			c.RedisAddr = net.JoinHostPort("127.0.0.1", redisPort)
			c.Env["REDIS_ADDR"] = c.RedisAddr
			missing = append(missing, "redis")
		} else {
			if !loopback(c.LiveAPI.Hostname()) || c.LiveAPI.Scheme != "http" || c.LiveAPI.Port() == "" {
				return errors.New("LiveKit ben ngoai khong truy cap duoc; de tu bat local, dat LIVEKIT_API_URL=http://127.0.0.1:7880")
			}
			c.LiveAPI.Host = net.JoinHostPort("127.0.0.1", livePort)
			c.Env["LIVEKIT_API_URL"] = c.LiveAPI.String()
			if loopback(c.LiveURL.Hostname()) {
				c.LiveURL.Host = c.LiveAPI.Host
				c.Env["LIVEKIT_URL"] = c.LiveURL.String()
			}
			missing = append(missing, "livekit")
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if err := c.composeEnv(); err != nil {
		return err
	}
	if err := s.Run(ctx, "docker", []string{"info", "--format", "{{.ServerVersion}}"}, c.Environ()); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("Docker chua san sang; mo Docker Desktop va thu lai")
	}
	// A dependency already listening above is never included in this list.
	s.owned = append([]string(nil), missing...)
	fmt.Fprintf(s.Output, "[dev] Khoi dong %s bang Docker Compose...\n", strings.Join(missing, ", "))
	args := append([]string{"up", "--detach", "--wait", "--wait-timeout", "90", "--no-deps"}, missing...)
	if err := s.compose(ctx, args...); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("khong bat duoc dependencies; xem loi Docker o tren (thu kiem tra cong 6379/7880/7881/7882)")
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if s.checkRedis(ctx) == nil && s.checkLiveKit(ctx) == nil {
			fmt.Fprintln(s.Output, "[dev] Redis va LiveKit da san sang.")
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("dependencies chua san sang; kiem tra REDIS_PASSWORD va LiveKit key/secret")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (s *Stack) Stop() error {
	if len(s.owned) == 0 {
		return nil
	}
	services := s.owned
	s.owned = nil
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	fmt.Fprintln(s.Output, "[dev] Dung cac container do luot chay nay khoi dong; giu nguyen Redis volume.")
	return s.compose(ctx, append([]string{"stop", "--timeout", "8"}, services...)...)
}

// StopProject is an explicit user request to stop this project's dependencies.
// Normal shutdown uses Stop instead, preserving any services that were reused.
func (s *Stack) StopProject() error {
	if err := s.Config.composeEnv(); err != nil {
		return err
	}
	s.owned = []string{"redis", "livekit"}
	return s.Stop()
}

func (s *Stack) checkRedis(ctx context.Context) error {
	client := redis.NewClient(&redis.Options{Addr: s.Config.RedisAddr, Password: s.Config.RedisPassword, DialTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second, MaxRetries: -1})
	defer client.Close()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return client.Ping(ctx).Err()
}

func (s *Stack) checkLiveKit(ctx context.Context) error {
	now := time.Now()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": s.Config.Key, "nbf": now.Add(-time.Second).Unix(), "exp": now.Add(time.Minute).Unix(),
		"video": jwt.MapClaims{"roomList": true},
	}).SignedString([]byte(s.Config.Secret))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Config.LiveAPI.String()+"/twirp/livekit.RoomService/ListRooms", bytes.NewBufferString("{}"))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("LiveKit room API not ready")
	}
	// A random HTTP server answering 200 is not a usable LiveKit room service.
	var rooms struct {
		Rooms []json.RawMessage `json:"rooms"`
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&rooms)
}
