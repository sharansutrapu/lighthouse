package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lighthouse/db"

	"golang.org/x/crypto/bcrypt"
)

func TestResetPasswordCLI(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	os.Setenv("DB_PATH", dbPath)
	defer os.Unsetenv("DB_PATH")

	if err := db.InitDB(dbPath); err != nil {
		t.Fatalf("init db: %v", err)
	}
	_, err := db.DB.Exec(
		`INSERT INTO users (username, password, is_admin, password_changed, password_version) VALUES (?, ?, 1, 1, 1)`,
		"admin", "old",
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	if err := runResetPasswordCLI([]string{"admin", "newpassword1"}); err != nil {
		t.Fatalf("reset password: %v", err)
	}

	var hash string
	var changed bool
	var version int
	err = db.DB.QueryRow(
		`SELECT password, password_changed, password_version FROM users WHERE username = 'admin'`,
	).Scan(&hash, &changed, &version)
	if err != nil {
		t.Fatalf("query user: %v", err)
	}
	if changed {
		t.Fatal("expected password_changed = 0 after CLI reset")
	}
	if version != 2 {
		t.Fatalf("expected password_version 2, got %d", version)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte("newpassword1")) != nil {
		t.Fatal("password hash does not match new password")
	}
}

func TestResetPasswordCLI_ValidationAndNotFoundBranches(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		dbPath  string
		wantErr string
	}{
		{name: "hostile: missing args", args: []string{"onlyone"}, wantErr: "usage:"},
		{name: "hostile: empty username", args: []string{"  ", "longenoughpw"}, wantErr: "username is required"},
		{name: "hostile: password too short", args: []string{"someuser", "short"}, wantErr: "password must be at least"},
		{name: "hostile: in-memory DB rejected", args: []string{"someuser", "longenoughpw"}, dbPath: ":memory:", wantErr: "in-memory database"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.dbPath != "" {
				os.Setenv("DB_PATH", tc.dbPath)
				defer os.Unsetenv("DB_PATH")
			}
			err := runResetPasswordCLI(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}

	t.Run("infra failure: user not found", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "notfound.db")
		os.Setenv("DB_PATH", dbPath)
		defer os.Unsetenv("DB_PATH")

		err := runResetPasswordCLI([]string{"ghost-user", "longenoughpassword"})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("expected 'not found' error, got %v", err)
		}
	})
}

