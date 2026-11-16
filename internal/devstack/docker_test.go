package devstack

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Opt in explicitly: this test creates containers and a disposable Redis volume.
// It never uses the repository's .env or the normal development Compose project.
func TestDockerLifecycle(t *testing.T) {
	if os.Getenv("DEVSTACK_DOCKER_TEST") != "1" {
		t.Skip("set DEVSTACK_DOCKER_TEST=1 with Docker running")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	// A listening Cloud endpoint lets us detect any unwanted startup probe.
	cloudListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer cloudListener.Close()
	assertCloudUntouched := func() {
		t.Helper()
		cloudListener.(*net.TCPListener).SetDeadline(time.Now().Add(50 * time.Millisecond))
		conn, err := cloudListener.Accept()
		if err == nil {
			conn.Close()
			t.Fatal("launcher contacted LiveKit Cloud")
		}
		if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
			t.Fatal(err)
		}
	}
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	env["REDIS_ADDR"] = "127.0.0.1:" + freePort(t)
	env["REDIS_PASSWORD"] = "disposable-test-password"
	env["LIVEKIT_URL"] = "wss://" + cloudListener.Addr().String()
	env["LIVEKIT_API_URL"] = ""
	env["LIVEKIT_API_KEY"], env["LIVEKIT_API_SECRET"] = "disposable-cloud-key", "disposable-cloud-secret"
	env["DEV_COMPOSE_PROJECT"] = "livemart-test-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	c, err := FromEnv(root, env)
	if err != nil {
		t.Fatal(err)
	}
	s := New(c, os.Stdout)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c.composeEnv()
		// Only the randomly named test project owns this volume and network.
		if err := s.compose(ctx, "down", "--volumes", "--timeout", "5"); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !s.owned {
		t.Fatal("expected missing Redis to be started")
	}
	assertCloudUntouched()
	for _, key := range []string{"LIVEKIT_URL", "LIVEKIT_API_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"} {
		if c.Env[key] != env[key] {
			t.Fatalf("%s changed while starting Redis", key)
		}
	}
	client := redis.NewClient(&redis.Options{Addr: c.RedisAddr, Password: c.RedisPassword})
	defer client.Close()
	if err := client.Set(ctx, "lifecycle-test", "retained", 0).Err(); err != nil {
		t.Fatal(err)
	}

	// Running dependencies must be authenticated and reused without any Docker call.
	reused := New(c, io.Discard)
	reused.Run = func(context.Context, string, []string, []string) error {
		t.Error("reused services should not invoke Docker")
		return nil
	}
	if err := reused.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := reused.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := s.checkRedis(ctx); err != nil {
		t.Fatal("reused Redis stopped", err)
	}
	assertCloudUntouched()
	badConfig := *c
	badConfig.RedisPassword = "incorrect-password"
	if err := New(&badConfig, io.Discard).Start(ctx); err == nil {
		t.Fatal("incorrect Redis password accepted")
	}

	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", c.RedisAddr, time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("owned Redis still listening after shutdown")
	}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	value, err := client.Get(ctx, "lifecycle-test").Result()
	if err != nil || value != "retained" {
		t.Fatal("Redis data lost on restart")
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	assertCloudUntouched()
}
