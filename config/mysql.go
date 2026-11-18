package config

import (
	"context"
	"errors"
	"log"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// mysqlDSN accepts the existing mysql:// URL and native Go driver DSNs.
// Never include the supplied connection string in a returned parse error.
func mysqlDSN(value string) (string, error) {
	var cfg *driver.Config
	var err error
	if strings.HasPrefix(value, "mysql://") {
		u, parseErr := url.Parse(value)
		if parseErr != nil || u.User == nil || u.Hostname() == "" || strings.TrimPrefix(u.Path, "/") == "" || u.Fragment != "" {
			return "", errors.New("invalid DATABASE_URL")
		}
		cfg = driver.NewConfig()
		cfg.User = u.User.Username()
		cfg.Passwd, _ = u.User.Password()
		port := u.Port()
		if port == "" {
			port = "3306"
		}
		cfg.Net, cfg.Addr, cfg.DBName = "tcp", net.JoinHostPort(u.Hostname(), port), strings.TrimPrefix(u.Path, "/")
		if u.RawQuery != "" {
			separator := "?"
			if strings.Contains(cfg.FormatDSN(), "?") {
				separator = "&"
			}
			cfg, err = driver.ParseDSN(cfg.FormatDSN() + separator + u.RawQuery)
		}
	} else {
		cfg, err = driver.ParseDSN(value)
	}
	if err != nil || cfg == nil || cfg.DBName == "" {
		return "", errors.New("invalid DATABASE_URL")
	}
	cfg.ParseTime, cfg.Loc = true, time.Local
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.Params == nil {
		cfg.Params = make(map[string]string)
	}
	cfg.Params["charset"] = "utf8mb4"
	return cfg.FormatDSN(), nil
}

func ConnectDatabase(ctx context.Context) error {
	value := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if value == "" {
		return errors.New("DATABASE_URL is not set")
	}
	dsn, err := mysqlDSN(value)
	if err != nil {
		return err
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		DisableAutomaticPing:                     true,
		Logger:                                   logger.New(log.New(os.Stdout, "\r\n", log.LstdFlags), logger.Config{SlowThreshold: 200 * time.Millisecond, LogLevel: logger.Info, IgnoreRecordNotFoundError: true, Colorful: true, ParameterizedQueries: true}),
	})
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		sqlDB.Close()
		return err
	}
	DB = db
	log.Println("MySQL connected")
	return nil
}
