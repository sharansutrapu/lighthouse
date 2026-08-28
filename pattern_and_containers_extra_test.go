package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"lighthouse/db"
)

// TestAppendValidatedPattern_Table closes gaps left by the existing
// TestAppendValidatedPattern (which only exercises the success path):
// patterns exceeding the length cap and invalid regex syntax must both be
// silently skipped rather than appended or panicking.
func TestAppendValidatedPattern_Table(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		wantGrown bool
	}{
		{name: "happy path: valid pattern is appended", pattern: "^valid$", wantGrown: true},
		{name: "hostile: pattern exceeds max length", pattern: strings.Repeat("a", maxContainerPatternLen+1), wantGrown: false},
		{name: "hostile: invalid regex syntax", pattern: "(unterminated[", wantGrown: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			base := []*regexp.Regexp{regexp.MustCompile("^base$")}
			out := appendValidatedPattern(base, tc.pattern)
			grew := len(out) > len(base)
			assert.Equal(t, tc.wantGrown, grew)
		})
	}
}

// TestExtractContainers_Table covers extractContainers' branches: direct
// array success, json.Marshal failure (unmarshalable input type), the
// wrapped-object fallback success, and the "nothing found" fallback.
func TestExtractContainers_Table(t *testing.T) {
	t.Run("happy path: direct array", func(t *testing.T) {
		in := []map[string]interface{}{{"Id": "c1"}}
		out := extractContainers(in)
		assert.Len(t, out, 1)
	})

	t.Run("hostile: value cannot be JSON-marshaled", func(t *testing.T) {
		out := extractContainers(make(chan int))
		assert.Nil(t, out)
	})

	t.Run("edge: wrapped object containing the array", func(t *testing.T) {
		wrapped := map[string]interface{}{
			"Items": []map[string]interface{}{{"Id": "c2"}},
		}
		out := extractContainers(wrapped)
		assert.Len(t, out, 1)
	})

	t.Run("edge: object with no array field found", func(t *testing.T) {
		wrapped := map[string]interface{}{"Count": 5}
		out := extractContainers(wrapped)
		assert.Nil(t, out)
	})

	t.Run("edge: bare JSON scalar has no array anywhere", func(t *testing.T) {
		out := extractContainers(42)
		assert.Nil(t, out)
	})
}

// TestLogAudit_Table covers the DB-write-failure branch (error is logged,
// not propagated/panicked) and the OnAuditLogged callback branch, neither of
// which any existing test exercises.
func TestLogAudit_Table(t *testing.T) {
	t.Run("happy path: successful write, no callback registered", func(t *testing.T) {
		assert.NoError(t, db.InitDB(":memory:"))
		db.OnAuditLogged = nil
		assert.NotPanics(t, func() {
			logAudit(1, "admin", "LOGIN", "System", "Success", "ok")
		})
	})

	t.Run("infra failure: DB write fails, error is only logged", func(t *testing.T) {
		assert.NoError(t, db.InitDB(":memory:"))
		db.GormDB.Migrator().DropTable(&db.AuditLog{})
		assert.NotPanics(t, func() {
			logAudit(1, "admin", "LOGIN", "System", "Success", "ok")
		})
	})

	t.Run("callback: OnAuditLogged is invoked with the right arguments", func(t *testing.T) {
		assert.NoError(t, db.InitDB(":memory:"))
		var gotAction, gotResource, gotStatus, gotMessage string
		db.OnAuditLogged = func(action, resource, status, message string) {
			gotAction, gotResource, gotStatus, gotMessage = action, resource, status, message
		}
		defer func() { db.OnAuditLogged = nil }()

		logAudit(1, "admin", "DELETE", "Container:abc", "Success", "removed")
		assert.Equal(t, "DELETE", gotAction)
		assert.Equal(t, "Container:abc", gotResource)
		assert.Equal(t, "Success", gotStatus)
		assert.Equal(t, "removed", gotMessage)
	})
}
