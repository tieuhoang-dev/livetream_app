package devstack

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

func localConfig(t *testing.T) *Config {
	t.Helper()
	c, err := FromEnv(".", map[string]string{"REDIS_ADDR": "127.0.0.1:" + freePort(t), "LIVEKIT_URL": "ws://127.0.0.1:" + freePort(t)})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRoomProbeAuthenticatesAndRejectsHTTPFailures(t *testing.T) {
	c := localConfig(t)
	status, body := http.StatusOK, `{"rooms":[]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/twirp/livekit.RoomService/ListRooms" {
			t.Error("incorrect room API request")
		}
		token, err := jwt.Parse(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), func(token *jwt.Token) (interface{}, error) { return []byte(c.Secret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(c.Key))
		if err != nil || !token.Valid {
			t.Error("invalid probe authentication")
		}
		claims, _ := token.Claims.(jwt.MapClaims)
		grant, _ := claims["video"].(map[string]interface{})
		if grant["roomList"] != true {
			t.Error("roomList grant missing")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	defer server.Close()
	c.LiveAPI, _ = origin(server.URL, "http", "https")
	s := New(c, io.Discard)
	if err := s.checkLiveKit(context.Background()); err != nil {
		t.Fatal(err)
	}
	status = http.StatusUnauthorized
	if err := s.checkLiveKit(context.Background()); err == nil {
		t.Fatal("authentication failure accepted")
	}
	status, body = http.StatusOK, "<html>another application</html>"
	if err := s.checkLiveKit(context.Background()); err == nil {
		t.Fatal("non-LiveKit HTTP response accepted")
	}
}

func TestOccupiedServiceNeverStartsDocker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "denied", http.StatusUnauthorized) }))
	defer server.Close()
	c := localConfig(t)
	c.LiveAPI, _ = origin(server.URL, "http", "https")
	s := New(c, io.Discard)
	s.Run = func(context.Context, string, []string, []string) error {
		t.Fatal("must not start Docker over an occupied service")
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
					return errors.New("one container could not start")
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
				t.Fatal("cleanup touched containers when Docker never started")
			}
			if !daemonFailure && !strings.Contains(joined, "stop --timeout 8 redis livekit") {
				t.Fatal("partial startup was not cleaned up")
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

func TestUnreachableRemoteIsNotReplaced(t *testing.T) {
	for _, service := range []string{"redis", "livekit"} {
		t.Run(service, func(t *testing.T) {
			c := localConfig(t)
			if service == "redis" {
				c.RedisAddr = "devstack-unreachable.invalid:6379"
			} else {
				c.LiveAPI, _ = origin("http://devstack-unreachable.invalid:7880", "http", "https")
			}
			s := New(c, io.Discard)
			s.Run = func(context.Context, string, []string, []string) error {
				t.Fatal("remote service must not be replaced by Docker")
				return nil
			}
			if err := s.Start(context.Background()); err == nil {
				t.Fatal("unreachable remote accepted")
			}
			if err := s.Stop(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
