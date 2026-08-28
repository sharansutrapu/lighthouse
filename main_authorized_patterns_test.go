package main

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"lighthouse/db"
)

func TestGetAuthorizedPatterns_Table(t *testing.T) {
	tests := []struct {
		name        string
		userID      int
		seedUser    *db.User
		seedTeam    *db.Team
		skipSeed    bool
		wantAllowed []string
		wantDenied  []string
	}{
		{
			name:        "infra failure: user not found denies everything",
			userID:      9001,
			skipSeed:    true,
			wantDenied:  []string{"anything"},
			wantAllowed: nil,
		},
		{
			name:        "happy path: unrestricted user matches everything",
			userID:      9002,
			seedUser:    &db.User{ID: 9002, IsRestrictedAccess: false},
			wantAllowed: []string{"anything-at-all", ""},
		},
		{
			name:        "happy path: restricted user with empty pattern denies everything",
			userID:      9003,
			seedUser:    &db.User{ID: 9003, IsRestrictedAccess: true, AllowedContainers: ""},
			wantDenied:  []string{"anything"},
			wantAllowed: nil,
		},
		{
			name:        "happy path: simple wildcard pattern auto-anchored",
			userID:      9004,
			seedUser:    &db.User{ID: 9004, IsRestrictedAccess: true, AllowedContainers: "web*"},
			wantAllowed: []string{"web-server", "web"},
			wantDenied:  []string{"database", "xweb"},
		},
		{
			name:        "happy path: explicit anchored pattern used as-is",
			userID:      9005,
			seedUser:    &db.User{ID: 9005, IsRestrictedAccess: true, AllowedContainers: "^db.*$"},
			wantAllowed: []string{"db-primary"},
			wantDenied:  []string{"web"},
		},
		{
			name:        "happy path: multiple comma-separated patterns with whitespace",
			userID:      9006,
			seedUser:    &db.User{ID: 9006, IsRestrictedAccess: true, AllowedContainers: " web* , db* ,,"},
			wantAllowed: []string{"web-1", "db-1"},
			wantDenied:  []string{"cache-1"},
		},
		{
			name:        "hostile: pattern with regex metacharacters is not auto-anchored",
			userID:      9007,
			seedUser:    &db.User{ID: 9007, IsRestrictedAccess: true, AllowedContainers: "(web|db)"},
			wantAllowed: []string{"my-web-1", "some-db-2"},
		},
		{
			name:        "hostile: invalid regex pattern is skipped, falls through to deny",
			userID:      9008,
			seedUser:    &db.User{ID: 9008, IsRestrictedAccess: true, AllowedContainers: "^(unterminated"},
			wantDenied:  []string{"anything"},
			wantAllowed: nil,
		},
		{
			name:     "happy path: team merges AllowedContainers when user's is empty",
			userID:   9009,
			seedTeam: &db.Team{ID: 900, Name: "team-a", AllowedContainers: "team*"},
			seedUser: &db.User{ID: 9009, IsRestrictedAccess: false, TeamID: teamIDPtr(900)},
			// Presence of a Team makes the user restricted regardless of IsRestrictedAccess.
			wantAllowed: []string{"team-service"},
			wantDenied:  []string{"other-service"},
		},
		{
			name:     "happy path: team merges AllowedContainers when user's is non-empty",
			userID:   9010,
			seedTeam: &db.Team{ID: 901, Name: "team-b", AllowedContainers: "shared*"},
			seedUser: &db.User{ID: 9010, IsRestrictedAccess: true, AllowedContainers: "solo*", TeamID: teamIDPtr(901)},
			wantAllowed: []string{"solo-1", "shared-1"},
			wantDenied:  []string{"other"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			if tc.seedTeam != nil {
				db.GormDB.Create(tc.seedTeam)
			}
			if !tc.skipSeed && tc.seedUser != nil {
				// Capture the intended values BEFORE Create(), since GORM's
				// `default:...` tags silently coerce Go zero-values (false,
				// "") on insert (and rewrite the in-memory struct to match).
				wantIsRestricted := tc.seedUser.IsRestrictedAccess
				wantAllowedContainers := tc.seedUser.AllowedContainers
				db.GormDB.Create(tc.seedUser)
				db.GormDB.Model(&db.User{}).Where("id = ?", tc.seedUser.ID).Updates(map[string]interface{}{
					"is_restricted_access": wantIsRestricted,
					"allowed_containers":   wantAllowedContainers,
				})
			}
			patterns := getAuthorizedPatterns(tc.userID)
			for _, name := range tc.wantAllowed {
				matched := false
				for _, p := range patterns {
					if p.MatchString(name) {
						matched = true
						break
					}
				}
				assert.Truef(t, matched, "expected %q to be allowed by patterns", name)
			}
			for _, name := range tc.wantDenied {
				for _, p := range patterns {
					assert.Falsef(t, p.MatchString(name), "expected %q to be denied, but pattern %q matched", name, p.String())
				}
			}
		})
	}
}

func teamIDPtr(id uint) *uint { return &id }

func TestGetAuthorizedPatterns_CacheHitAvoidsSecondDBLookup(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	db.GormDB.Create(&db.User{ID: 9100, IsRestrictedAccess: true, AllowedContainers: "web*"})

	first := getAuthorizedPatterns(9100)
	assert.NotEmpty(t, first)

	// Mutate the DB row directly; if the cache is honored, the second call
	// must still reflect the OLD patterns (web*), not the new value.
	db.GormDB.Model(&db.User{}).Where("id = ?", 9100).Update("allowed_containers", "db*")

	second := getAuthorizedPatterns(9100)
	assert.True(t, second[0].MatchString("web-1"), "expected cached pattern to still match the original allowed_containers value")
}

func TestGetAuthorizedPatterns_ExpiredCacheRefetches(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	db.GormDB.Create(&db.User{ID: 9101, IsRestrictedAccess: true, AllowedContainers: "web*"})
	_ = getAuthorizedPatterns(9101)

	// Force the cached entry to look expired.
	patternCache.Store(9101, cachedPattern{patterns: []*regexp.Regexp{}, expiry: time.Now().Add(-time.Second)})
	db.GormDB.Model(&db.User{}).Where("id = ?", 9101).Update("allowed_containers", "db*")

	refreshed := getAuthorizedPatterns(9101)
	assert.True(t, refreshed[0].MatchString("db-1"))
	assert.False(t, refreshed[0].MatchString("web-1"))
}

func TestAppendValidatedPattern_TooLongIsSkipped(t *testing.T) {
	longPattern := "^" + strings.Repeat("a", maxContainerPatternLen+10) + "$"
	patterns := appendValidatedPattern(nil, longPattern)
	assert.Empty(t, patterns)
}
