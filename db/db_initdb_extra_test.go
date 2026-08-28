package db

import (
	"os"
	"path/filepath"
	"testing"
)

// clearDBEnv unsets DB_TYPE/DB_DSN so InitDB exercises its own defaulting
// logic instead of the values every other test in this package pins.
func clearDBEnv(t *testing.T) {
	t.Helper()
	origType, hadType := os.LookupEnv("DB_TYPE")
	origDSN, hadDSN := os.LookupEnv("DB_DSN")
	os.Unsetenv("DB_TYPE")
	os.Unsetenv("DB_DSN")
	t.Cleanup(func() {
		if hadType {
			os.Setenv("DB_TYPE", origType)
		} else {
			os.Unsetenv("DB_TYPE")
		}
		if hadDSN {
			os.Setenv("DB_DSN", origDSN)
		} else {
			os.Unsetenv("DB_DSN")
		}
	})
}

// TestInitDB_DefaultsWhenEnvUnset covers the "DB_TYPE/DB_DSN default to
// sqlite/dataSourceName" branches — every other test in this package pins
// both env vars explicitly, which was leaving these defaulting branches at
// 0% coverage.
func TestInitDB_DefaultsWhenEnvUnset(t *testing.T) {
	clearDBEnv(t)

	if err := InitDB(":memory:"); err != nil {
		t.Fatalf("InitDB with unset DB_TYPE/DB_DSN should default to sqlite :memory:, got error: %v", err)
	}
	if GormDB == nil || DB == nil {
		t.Fatal("expected GormDB/DB to be initialized via the default sqlite path")
	}
}

// TestInitDB_OpenError covers the gorm.Open error branch: an invalid target
// path (inside a directory that does not exist) makes the sqlite driver fail
// to open the file.
func TestInitDB_OpenError(t *testing.T) {
	os.Setenv("DB_TYPE", "sqlite")
	badPath := filepath.Join(t.TempDir(), "does-not-exist", "nested", "impossible.db")
	os.Setenv("DB_DSN", badPath)
	defer func() {
		os.Setenv("DB_TYPE", "sqlite")
		os.Setenv("DB_DSN", ":memory:")
	}()

	if err := InitDB(badPath); err == nil {
		t.Fatal("InitDB with an unwritable nested path should fail, got nil error")
	}
}

// TestInitDB_AutoMigrateError covers the AutoMigrate failure branch: pointing
// at a file that exists but is not a valid SQLite database makes the first
// DDL statement AutoMigrate issues fail.
func TestInitDB_AutoMigrateError(t *testing.T) {
	corrupt := filepath.Join(t.TempDir(), "corrupt.db")
	if err := os.WriteFile(corrupt, []byte("this is not a sqlite database file"), 0644); err != nil {
		t.Fatalf("failed to write corrupt db file: %v", err)
	}

	os.Setenv("DB_TYPE", "sqlite")
	os.Setenv("DB_DSN", corrupt)
	defer func() {
		os.Setenv("DB_TYPE", "sqlite")
		os.Setenv("DB_DSN", ":memory:")
	}()

	if err := InitDB(corrupt); err == nil {
		t.Fatal("InitDB against a corrupted sqlite file should fail during AutoMigrate, got nil error")
	}
}
