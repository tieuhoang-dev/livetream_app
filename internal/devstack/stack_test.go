package devstack

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
)

func freePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func localConfig(t *testing.T) *Config {
	t.Helper()
	c, err := FromEnv(".", map[string]string{"REDIS_ADDR": "127.0.0.1:" + freePort(t)})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestOccupiedServiceNeverStartsDocker(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			io.WriteString(conn, "-ERR not a Redis service\r\n")
			conn.Close()
		}
	}()
	c := localConfig(t)
	c.RedisAddr = listener.Addr().String()
	s := New(c, io.Discard)
	s.Run = func(context.Context, string, []string, []string) error {
		t.Fatal("must not start Docker over occupied Redis")
		return nil
	}
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("invalid occupied service accepted")
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestPartialStartupCleanupAndDockerUnavailable(t *testing.T) {
	for _, daemonFailure := range []bool{false, true} {
		t.Run(strconv.FormatBool(daemonFailure), func(t *testing.T) {
			s := New(localConfig(t), io.Discard)
			var commands []string
			s.Run = func(_ context.Context, _ string, args, _ []string) error {
				command := strings.Join(args, " ")
				commands = append(commands, command)
				if strings.HasPrefix(command, "info") && daemonFailure {
					return errors.New("daemon unavailable")
				}
				if strings.Contains(command, " up ") {
					return errors.New("container could not start")
				}
				return nil
			}
			if err := s.Start(context.Background()); err == nil {
				t.Fatal("startup failure missing")
			}
			if err := s.Stop(); err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(commands, "\n")
			if daemonFailure && len(commands) != 1 {
				t.Fatal("cleanup touched containers before Docker started")
			}
			if !daemonFailure && !strings.Contains(joined, "stop --timeout 8 redis") {
				t.Fatal("partial startup not cleaned up")
			}
			if strings.Contains(joined, "livekit") {
				t.Fatal("Docker command attempted to manage LiveKit")
			}
			before := len(commands)
			s.Stop()
			if len(commands) != before {
				t.Fatal("cleanup should be idempotent")
			}
		})
	}
}

func TestCancelledStartDoesNotInvokeDocker(t *testing.T) {
	s := New(localConfig(t), io.Discard)
	s.Run = func(context.Context, string, []string, []string) error {
		t.Fatal("Docker called with canceled context")
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(s.Start(ctx), context.Canceled) {
		t.Fatal("cancellation not propagated")
	}
}

func TestUnreachableRemoteRedisIsNotReplaced(t *testing.T) {
	c := localConfig(t)
	c.RedisAddr = "devstack-unreachable.invalid:6379"
	s := New(c, io.Discard)
	s.Run = func(context.Context, string, []string, []string) error {
		t.Fatal("remote Redis must not be replaced by Docker")
		return nil
	}
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("unreachable remote accepted")
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
}
