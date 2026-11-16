// Command dev starts local Redis/LiveKit dependencies, then the host API.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"livesteam_app/internal/devstack"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "[dev]", err)
		os.Exit(1)
	}
}

func run() (result error) {
	servicesOnly := flag.Bool("services-only", false, "start Redis/LiveKit and leave them running, without the API")
	stop := flag.Bool("stop", false, "stop this Compose project's Redis/LiveKit, keeping Redis data")
	flag.Parse()
	if *servicesOnly && *stop {
		return errors.New("chi chon mot trong -services-only va -stop")
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, "compose.dev.yml")); err != nil {
		return errors.New("chay lenh tu thu muc goc repo (noi co go.mod va compose.dev.yml)")
	}
	cfg, err := devstack.Load(root)
	if err != nil {
		return err
	}
	stack := devstack.New(cfg, os.Stdout)
	if *stop {
		return stack.StopProject()
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if !*servicesOnly {
		for _, key := range []string{"DATABASE_URL", "MONGO_URI", "JWT_SECRET"} {
			if cfg.Env[key] == "" {
				return fmt.Errorf("dien %s trong .env truoc", key)
			}
		}
	}
	// Install cleanup before starting so partially started containers are stopped too.
	keep := false
	defer func() {
		if ctx.Err() != nil && errors.Is(result, context.Canceled) {
			result = nil
		}
		if !keep {
			if err := stack.Stop(); err != nil {
				result = errors.Join(result, fmt.Errorf("khong dung duoc container; chay go run ./cmd/dev -stop: %w", err))
			}
		}
	}()
	if err := stack.Start(ctx); err != nil {
		return err
	}
	if *servicesOnly {
		keep = true
		fmt.Println("[dev] Dependencies dang chay. Dung bang: go run ./cmd/dev -stop")
		return nil
	}
	binDir := filepath.Join(root, ".dev")
	if err := os.MkdirAll(binDir, 0700); err != nil {
		return err
	}
	name := "livestream-api"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(binDir, name)
	fmt.Println("[dev] Build API...")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd")
	build.Dir, build.Env = root, cfg.Environ()
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("build API that bai: %w", err)
	}
	// Run the binary directly so cancellation cannot leave a go-run child behind.
	fmt.Println("[dev] Chay API; Ctrl+C de dung API va dependencies vua bat.")
	api := exec.CommandContext(ctx, binary)
	api.Dir, api.Env = root, cfg.Environ()
	api.Stdin, api.Stdout, api.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := api.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("API da dung: %w", err)
	}
	return nil
}
