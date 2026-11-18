package app

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type settings struct{ address string }

func loadSettings() (settings, error) {
	// Environment-only deployments do not need a .env file. Existing environment
	// variables retain precedence over values loaded from the local file.
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return settings{}, fmt.Errorf("load .env: %w", err)
	}
	for _, key := range []string{"DATABASE_URL", "MONGO_URI", "JWT_SECRET"} {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			return settings{}, fmt.Errorf("%s is not set", key)
		}
	}
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return settings{}, errors.New("PORT must be an integer from 1 to 65535")
	}
	return settings{address: fmt.Sprintf(":%d", number)}, nil
}
