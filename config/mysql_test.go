package config

import (
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestMySQLDSN(t *testing.T) {
	for _, tc := range []struct{ name, input, address, password string }{
		{"existing URL", "mysql://root:sample@127.0.0.1:3307/live", "127.0.0.1:3307", "sample"},
		{"escaped password", "mysql://root:p%40ss%2Fword@localhost/live", "localhost:3306", "p@ss/word"},
		{"IPv6", "mysql://root:sample@[::1]:3306/live", "[::1]:3306", "sample"},
		{"native DSN", "root:sample@tcp(localhost:3306)/live?parseTime=false", "localhost:3306", "sample"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, err := mysqlDSN(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := mysql.ParseDSN(value)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Addr != tc.address || cfg.Passwd != tc.password || cfg.DBName != "live" || !cfg.ParseTime || cfg.Loc != time.Local || cfg.Params["charset"] != "utf8mb4" {
				t.Fatal("connection settings changed")
			}
		})
	}
	for _, value := range []string{"", "mysql://root:private-secret@/live", "mysql://root:private-secret@host", "mysql://root:private-secret@host:notaport/live", "invalid-native-dsn"} {
		if _, err := mysqlDSN(value); err == nil || err.Error() != "invalid DATABASE_URL" {
			t.Fatal("invalid connection string must return a generic error")
		}
	}
}
