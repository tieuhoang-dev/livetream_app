package devstack

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"time"

	"github.com/redis/go-redis/v9"
)

type Runner func(context.Context, string, []string, []string) error

type Stack struct {
	Config *Config
	Output io.Writer
	Run    Runner
	owned  bool
}

func New(c *Config, output io.Writer) *Stack {
	stack := &Stack{Config: c, Output: output}
	stack.Run = func(ctx context.Context, program string, args []string, env []string) error {
		command := exec.CommandContext(ctx, program, args...)
		command.Dir, command.Env = c.Root, env
		command.Stdout, command.Stderr = output, output
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
	// Authenticate occupied Redis endpoints. LiveKit Cloud is not contacted
	// or started by this launcher; its configuration belongs to the API.
	host, port, _ := net.SplitHostPort(c.RedisAddr)
	connection, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", c.RedisAddr)
	if err == nil {
		connection.Close()
		if err := s.checkRedis(ctx); err != nil {
			return errors.New("Redis dang co dich vu nhung kiem tra xac thuc that bai; kiem tra REDIS_PASSWORD trong .env")
		}
		fmt.Fprintf(s.Output, "[dev] Dung Redis dang chay tai %s\n", c.RedisAddr)
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !loopback(host) {
		return errors.New("Redis ben ngoai khong truy cap duoc; khong khoi dong Redis local thay the")
	}
	c.RedisAddr = net.JoinHostPort("127.0.0.1", port)
	c.Env["REDIS_ADDR"] = c.RedisAddr
	c.composeEnv()
	if err := s.Run(ctx, "docker", []string{"info", "--format", "{{.ServerVersion}}"}, c.Environ()); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("Docker chua san sang; mo Docker Desktop va thu lai")
	}
	// Register ownership before Compose so cleanup also handles partial startup.
	s.owned = true
	fmt.Fprintln(s.Output, "[dev] Khoi dong Redis bang Docker Compose...")
	if err := s.compose(ctx, "up", "--detach", "--wait", "--wait-timeout", "90", "--no-deps", "redis"); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("khong bat duoc Redis; xem loi Docker o tren va kiem tra cong REDIS_ADDR")
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if s.checkRedis(ctx) == nil {
			fmt.Fprintln(s.Output, "[dev] Redis da san sang.")
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("Redis chua san sang; kiem tra REDIS_PASSWORD")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (s *Stack) Stop() error {
	if !s.owned {
		return nil
	}
	s.owned = false
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	fmt.Fprintln(s.Output, "[dev] Dung Redis container; giu nguyen Redis volume.")
	return s.compose(ctx, "stop", "--timeout", "8", "redis")
}

// StopProject explicitly stops this project's Redis. Normal shutdown uses Stop
// to preserve Redis instances running before this invocation.
func (s *Stack) StopProject() error {
	s.Config.composeEnv()
	s.owned = true
	return s.Stop()
}

func (s *Stack) checkRedis(ctx context.Context) error {
	client := redis.NewClient(&redis.Options{Addr: s.Config.RedisAddr, Password: s.Config.RedisPassword, DialTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second, MaxRetries: -1})
	defer client.Close()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return client.Ping(ctx).Err()
}
