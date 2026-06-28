package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"gorm.io/gorm"
	"log"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"lighthouse/alerts"
	"lighthouse/archival"
	"lighthouse/backup"
	"lighthouse/cluster"
	"lighthouse/db"
	"lighthouse/gitops"
	"lighthouse/scanner"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	echojwt "github.com/labstack/echo-jwt/v4"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/time/rate"
)

var (
	SECRET_KEY        []byte // set by initSecretKey() from SECRET_KEY env var; never hardcoded
	maxPasswordLength = 128
	upgrader          = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	CanStart         bool
	CanStop          bool
	CanRestart       bool
	CanDelete        bool
	AllowShell       bool
	pendingAuthCodes sync.Map
	LighthouseMode   string
	NodeID           string
)

func generateSecureCode() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

type Container struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Image      string  `json:"image"`
	State      string  `json:"state"`
	Created    int64   `json:"created"`
	Status     string  `json:"status"`
	CPULimit   float64 `json:"cpu_limit"`
	MemLimit   int64   `json:"mem_limit"`
	CPU        float64 `json:"cpu"`
	Memory     int64   `json:"memory"`
	SizeRw     int64   `json:"size_rw"`
	SizeRootFs int64   `json:"size_root_fs"`
	IsPlatform bool    `json:"is_platform"` // true when this is the LightHouse container itself
}

type UserClaims struct {
	ID                   int    `json:"id"`
	Username             string `json:"username"`
	IsAdmin              bool   `json:"is_admin"`
	IsRestrictedAccess   bool   `json:"is_restricted_access"`
	CanStart             bool   `json:"can_start"`
	CanStop              bool   `json:"can_stop"`
	CanRestart           bool   `json:"can_restart"`
	CanDelete            bool   `json:"can_delete"`
	CanShell             bool   `json:"can_shell"`
	CanViewSystemHealth  bool   `json:"can_view_system_health"`
	CanRunScans          bool   `json:"can_run_scans"`
	CanCreateDeployments bool   `json:"can_create_deployments"`
	CanEditDeployments   bool   `json:"can_edit_deployments"`
	CanDeleteDeployments bool   `json:"can_delete_deployments"`
	AllowedContainers    string `json:"allowed_containers"`
	IsActive             bool   `json:"is_active"`
	PasswordChanged      bool   `json:"password_changed"`
	PasswordVersion      int    `json:"password_version"`
	TokenType            string `json:"token_type,omitempty"`
	jwt.RegisteredClaims
}

type User struct {
	ID                 int    `json:"id"`
	Username           string `json:"username"`
	IsAdmin            bool   `json:"is_admin"`
	PasswordChanged    bool   `json:"password_changed"`
	CanStart           bool   `json:"can_start"`
	CanStop            bool   `json:"can_stop"`
	CanRestart         bool   `json:"can_restart"`
	CanDelete          bool   `json:"can_delete"`
	CanShell           bool   `json:"can_shell"`
	IsRestrictedAccess bool   `json:"is_restricted_access"`
	AllowedContainers  string `json:"allowed_containers"`
	IsActive           bool   `json:"is_active"`
}

func logAudit(userID int, username, action, resource, status, message string) {
	entry := db.AuditLog{
		UserID:   uint(userID),
		Username: username,
		Action:   action,
		Resource: resource,
		Status:   status,
		Message:  message,
	}
	if err := db.GormDB.Create(&entry).Error; err != nil {
		log.Printf("Failed to write audit log: %v", err)
	}

	if db.OnAuditLogged != nil {
		db.OnAuditLogged(action, resource, status, message)
	}
}

func getAuthorizedPatterns(userID int) []string {
	var user db.User
	if err := db.GormDB.Preload("Team").First(&user, userID).Error; err != nil {
		return []string{"^$"}
	}
	isRestricted := user.IsRestrictedAccess || (user.Team != nil)

	allowedContainers := user.AllowedContainers
	if user.Team != nil && user.Team.AllowedContainers != "" {
		if allowedContainers == "" || allowedContainers == ".*" {
			allowedContainers = user.Team.AllowedContainers
		} else {
			allowedContainers = allowedContainers + "," + user.Team.AllowedContainers
		}
	}
	pattern := allowedContainers

	if !isRestricted {
		return []string{".*"}
	}

	if pattern == "" {
		return []string{""}
	}

	rawPatterns := strings.Split(pattern, ",")
	var finalPatterns []string
	for _, p := range rawPatterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		if strings.HasPrefix(p, "^") || strings.HasSuffix(p, "$") {
			finalPatterns = appendValidatedPattern(finalPatterns, p)
			continue
		}

		regP := strings.ReplaceAll(p, "*", ".*")
		regP = strings.ReplaceAll(regP, "..*", ".*")

		if !strings.ContainsAny(regP, "()[]{}|") {
			if !strings.HasPrefix(regP, "^") {
				regP = "^" + regP
			}
			if !strings.HasSuffix(regP, "$") {
				regP = regP + "$"
			}
		}
		finalPatterns = appendValidatedPattern(finalPatterns, regP)
	}
	return finalPatterns
}

func appendValidatedPattern(patterns []string, regP string) []string {
	if len(regP) > maxContainerPatternLen {
		log.Printf("Skipping container pattern: exceeds %d characters", maxContainerPatternLen)
		return patterns
	}
	if _, err := regexp.Compile(regP); err != nil {
		log.Printf("Skipping invalid container pattern: %v", err)
		return patterns
	}
	return append(patterns, regP)
}

func main() {
	if exit, code := dispatchCLI(os.Args); exit {
		os.Exit(code)
	}

	LighthouseMode = os.Getenv("LIGHTHOUSE_MODE")
	if LighthouseMode == "" {
		LighthouseMode = "standalone"
	}
	NodeID = os.Getenv("NODE_NAME")
	if NodeID == "" {
		host, _ := os.Hostname()
		NodeID = host
	}

	logRunMode()
	initSecretKey()
	initClientAccess()
	initWSUpgrader()

	getEnvBool := func(key string, defaultVal bool) bool {
		val := os.Getenv(key)
		if val == "" {
			return defaultVal
		}
		return val == "true"
	}

	CanStart = getEnvBool("ALLOW_START", false)
	CanStop = getEnvBool("ALLOW_STOP", false)
	CanRestart = getEnvBool("ALLOW_RESTART", false)
	CanDelete = getEnvBool("ALLOW_DELETE", false)
	AllowShell = getEnvBool("ALLOW_SHELL", false) || getEnvBool("ALLOW_BASH", false)
	initExcludedContainers()

	// DB Init
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "/app/data/lighthouse.db"
	}

	if dbPath != ":memory:" {
		dir := filepath.Dir(dbPath)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				log.Fatalf("Failed to create database directory: %v", err)
			}
		}
	}

	if err := db.InitDB(dbPath); err != nil {
		log.Fatalf("Failed to init DB: %v", err)
	}

	// Migrate any existing bad memory thresholds (from bytes to MB)
	// If a threshold is > 1,000,000, it's virtually impossible to be Megabytes (>1 Terabyte),
	// so it was likely saved in bytes by the old default seeding script.
	db.GormDB.Exec("UPDATE alert_rules SET metric_mem_threshold = metric_mem_threshold / (1024*1024) WHERE metric_mem_threshold > 1000000")

	// Seed Admin
	seedAdmin()
	cleanupStaleAlerts()

	e := echo.New()
	if TrustProxy {
		e.IPExtractor = echo.ExtractIPFromXFFHeader()
	}
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(securityHeadersMiddleware())
	e.Use(middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(rate.Limit(50))))
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOriginFunc: func(origin string) (bool, error) {
			return corsOriginAllowed(origin), nil
		},
		AllowMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodDelete,
			http.MethodOptions,
		},
		AllowHeaders: []string{
			echo.HeaderAuthorization,
			echo.HeaderContentType,
			headerLightHouseClient,
		},
	}))
	e.Use(clientAccessMiddleware())

	// Docker Client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("Failed to create Docker client: %v", err)
	}

	// Start Background Collector
	startStatsCollector(cli)

	// Start Alerting Engine
	alertMgr := alerts.NewAlertManager(cli)
	alertMgr.Start()

	// Start background backup scheduler
	backup.InitScheduler()

	// Start background archival scheduler
	archival.InitScheduler()

	gitops.StartManager()
	defer alertMgr.Stop()

	if LighthouseMode == "spoke" {
		hubURL := os.Getenv("HUB_URL")
		hubToken := os.Getenv("HUB_TOKEN")
		if hubURL == "" || hubToken == "" {
			log.Fatalf("Spoke mode requires HUB_URL and HUB_TOKEN")
		}

		// In spoke mode, the API server doesn't run, we just connect to hub and wait
		log.Printf("Starting Spoke mode connected to %s", hubURL)

		cluster.StartSpokeAgent(hubURL, hubToken, NodeID, cli)
		return
	}

	if LighthouseMode == "hub" {
		hubToken := os.Getenv("HUB_TOKEN")
		if hubToken == "" {
			log.Fatalf("Hub mode requires HUB_TOKEN")
		}
		cluster.RegisterHubRoutes(e, hubToken)
	}

	// Auth Endpoints
	e.GET("/auth/google", func(c echo.Context) error {
		var setting db.Setting
		db.GormDB.Select("google_client_id", "google_client_secret").First(&setting, 1)
		clientID := setting.GoogleClientID
		clientSecret := setting.GoogleClientSecret
		if clientID == "" || clientSecret == "" {
			return c.String(http.StatusInternalServerError, "Google OAuth is not configured")
		}
		inviteToken := c.QueryParam("invite_token")
		stateBytes := make([]byte, 16)
		rand.Read(stateBytes)
		state := hex.EncodeToString(stateBytes)
		if inviteToken != "" {
			state = state + ":" + inviteToken
		}

		// Set cookie for state validation
		c.SetCookie(&http.Cookie{
			Name:     "oauth_state",
			Value:    state,
			Expires:  time.Now().Add(10 * time.Minute),
			HttpOnly: true,
			Secure:   requestScheme(c.Request()) == "https",
			SameSite: http.SameSiteLaxMode,
			Path:     "/",
		})

		conf := &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  fmt.Sprintf("%s://%s/auth/google/callback", requestScheme(c.Request()), requestHost(c.Request())),
			Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile"},
			Endpoint:     google.Endpoint,
		}
		url := conf.AuthCodeURL(state)
		return c.Redirect(http.StatusTemporaryRedirect, url)
	})

	e.GET("/auth/google/callback", func(c echo.Context) error {
		stateCookie, err := c.Cookie("oauth_state")
		if err != nil || c.QueryParam("state") != stateCookie.Value {
			return c.Redirect(http.StatusTemporaryRedirect, "/?error="+url.QueryEscape("Invalid OAuth state. Please try again."))
		}
		stateParts := strings.Split(stateCookie.Value, ":")
		inviteToken := ""
		if len(stateParts) == 2 {
			inviteToken = stateParts[1]
		}

		var setting db.Setting
		db.GormDB.Select("google_client_id", "google_client_secret").First(&setting, 1)
		clientID := setting.GoogleClientID
		clientSecret := setting.GoogleClientSecret
		conf := &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  fmt.Sprintf("%s://%s/auth/google/callback", requestScheme(c.Request()), requestHost(c.Request())),
			Endpoint:     google.Endpoint,
		}

		tok, err := conf.Exchange(context.Background(), c.QueryParam("code"))
		if err != nil {
			return c.Redirect(http.StatusTemporaryRedirect, "/?error="+url.QueryEscape("Failed to exchange token with Google."))
		}

		client := conf.Client(context.Background(), tok)
		resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
		if err != nil {
			return c.Redirect(http.StatusTemporaryRedirect, "/?error="+url.QueryEscape("Failed to get user info from Google."))
		}
		defer resp.Body.Close()

		var userInfo struct {
			Id    string `json:"id"`
			Email string `json:"email"`
			Name  string `json:"name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
			return c.Redirect(http.StatusTemporaryRedirect, "/?error="+url.QueryEscape("Failed to parse user info from Google."))
		}

		nameToUse := userInfo.Name
		if nameToUse == "" {
			nameToUse = userInfo.Email
		}

		// Look up user by email
		var user db.User
		err = db.GormDB.Preload("Team").Where("email = ?", userInfo.Email).First(&user).Error

		if err == gorm.ErrRecordNotFound {
			// Check if this is the first user (bootstrap Admin)
			var count int64
			db.GormDB.Model(&db.User{}).Count(&count)
			isFirstUser := (count == 0)

			if !isFirstUser {
				return c.Redirect(http.StatusTemporaryRedirect, "/?error="+url.QueryEscape("Unauthorized: You must be invited to join LightHouse."))
			}

			newUser := db.User{
				Username:           nameToUse,
				Email:              userInfo.Email,
				GoogleID:           userInfo.Id,
				IsAdmin:            isFirstUser,
				IsActive:           true,
				CanStart:           isFirstUser,
				CanStop:            isFirstUser,
				CanRestart:         isFirstUser,
				CanDelete:          isFirstUser,
				CanShell:           isFirstUser,
				IsRestrictedAccess: !isFirstUser,
			}
			err = db.GormDB.Create(&newUser).Error

			if err == nil {
				user = newUser
			} else {
				return c.Redirect(http.StatusTemporaryRedirect, "/?error="+url.QueryEscape("Failed to bootstrap admin user."))
			}
		} else if err != nil {
			return c.Redirect(http.StatusTemporaryRedirect, "/?error="+url.QueryEscape("Internal database error."))
		}

		isAdmin := user.IsAdmin
		isActive := user.IsActive
		dbInviteToken := user.InviteToken
		dbExpiresAt := user.InviteExpiresAt
		dbGoogleID := user.GoogleID
		id := int(user.ID)

		// Handle invite logic
		if dbInviteToken != "" {
			if inviteToken != dbInviteToken {
				return c.Redirect(http.StatusTemporaryRedirect, "/?error="+url.QueryEscape("Invalid invite token."))
			}
			if dbExpiresAt != nil && time.Now().After(*dbExpiresAt) {
				return c.Redirect(http.StatusTemporaryRedirect, "/?error="+url.QueryEscape("Your invite link has expired."))
			}
			// Mark invite as used, bind google ID, and update username
			db.GormDB.Model(&user).Updates(map[string]interface{}{
				"invite_token":      nil,
				"invite_expires_at": nil,
				"google_id":         userInfo.Id,
				"username":          nameToUse,
			})
		} else if dbGoogleID != userInfo.Id {
			// Existing user, update google id and username if empty
			if dbGoogleID == "" {
				db.GormDB.Model(&user).Updates(map[string]interface{}{
					"google_id": userInfo.Id,
					"username":  nameToUse,
				})
			} else {
				// Mismatch google ID
				return c.Redirect(http.StatusTemporaryRedirect, "/?error="+url.QueryEscape("This Google account does not match the one registered with your profile."))
			}
		}

		if !isActive {
			return c.String(http.StatusForbidden, "Account deactivated")
		}

		allowedContainers := user.AllowedContainers
		if user.Team != nil && user.Team.AllowedContainers != "" {
			if allowedContainers == "" || allowedContainers == ".*" {
				allowedContainers = user.Team.AllowedContainers
			} else {
				allowedContainers = allowedContainers + "," + user.Team.AllowedContainers
			}
		}

		canStart, canStop, canRestart, canDelete, canShell := user.CanStart, user.CanStop, user.CanRestart, user.CanDelete, user.CanShell
		canViewHealth, canRunScans := user.CanViewSystemHealth, user.CanRunScans
		canCreateDep, canEditDep, canDeleteDep := user.CanCreateDeployments, user.CanEditDeployments, user.CanDeleteDeployments

		if user.Team != nil {
			canStart = user.Team.CanStart
			canStop = user.Team.CanStop
			canRestart = user.Team.CanRestart
			canDelete = user.Team.CanDelete
			canShell = user.Team.CanShell
			canViewHealth = user.Team.CanViewSystemHealth
			canRunScans = user.Team.CanRunScans
			canCreateDep = user.Team.CanCreateDeployments
			canEditDep = user.Team.CanEditDeployments
			canDeleteDep = user.Team.CanDeleteDeployments
		}

		claims := &UserClaims{
			ID:                   id,
			Username:             userInfo.Email, // Store email as username
			IsAdmin:              user.IsAdmin,
			CanStart:             canStart,
			CanStop:              canStop,
			CanRestart:           canRestart,
			CanDelete:            canDelete,
			CanShell:             canShell,
			CanViewSystemHealth:  canViewHealth,
			CanRunScans:          canRunScans,
			CanCreateDeployments: canCreateDep,
			CanEditDeployments:   canEditDep,
			CanDeleteDeployments: canDeleteDep,
			IsRestrictedAccess:   user.IsRestrictedAccess,
			AllowedContainers:    allowedContainers,
			PasswordVersion:      user.PasswordVersion,
			IsActive:             isActive,
		}

		accessToken, refreshToken, err := issueTokenPair(claims)
		if err != nil {
			return c.String(http.StatusInternalServerError, "Failed to generate token")
		}

		code := generateSecureCode()
		pendingAuthCodes.Store(code, map[string]interface{}{
			"access_token":     accessToken,
			"refresh_token":    refreshToken,
			"is_admin":         isAdmin,
			"password_changed": true, // Assume OAuth users don't need to change local password
		})

		go func(c string) {
			time.Sleep(60 * time.Second)
			pendingAuthCodes.Delete(c)
		}(code)

		return c.Redirect(http.StatusTemporaryRedirect, fmt.Sprintf("/?code=%s", code))
	})
	e.POST("/api/token/exchange", func(c echo.Context) error {
		code := c.FormValue("code")
		if code == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Missing code"})
		}
		data, ok := pendingAuthCodes.LoadAndDelete(code)
		if !ok {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid or expired code"})
		}
		return c.JSON(http.StatusOK, data)
	})

	e.POST("/api/token", func(c echo.Context) error {
		ip := c.RealIP()
		if loginRateLimit.isLimited(ip, 10, 15*time.Minute) {
			alerts.Global.TriggerSystemAlert("system:auth_failed", fmt.Sprintf("Multiple failed login attempts detected from IP: %s", ip))
			return c.JSON(http.StatusTooManyRequests, map[string]string{"error": "Too many login attempts. Try again later."})
		}

		username := strings.TrimSpace(c.FormValue("username"))
		password := c.FormValue("password")

		if len(password) > maxPasswordLength {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("Password must not exceed %d characters", maxPasswordLength)})
		}

		var user db.User
		err := db.GormDB.Preload("Team").Where("username = ?", username).First(&user).Error
		if err != nil || bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)) != nil {
			loginRateLimit.recordFailure(ip)
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid credentials"})
		}

		loginRateLimit.clear(ip)

		if !user.IsActive {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Account deactivated. Please contact administrator."})
		}

		allowedContainers := user.AllowedContainers
		if user.Team != nil && user.Team.AllowedContainers != "" {
			if allowedContainers == "" || allowedContainers == ".*" {
				allowedContainers = user.Team.AllowedContainers
			} else {
				allowedContainers = allowedContainers + "," + user.Team.AllowedContainers
			}
		}

		canStart, canStop, canRestart, canDelete, canShell := user.CanStart, user.CanStop, user.CanRestart, user.CanDelete, user.CanShell
		canViewHealth, canRunScans := user.CanViewSystemHealth, user.CanRunScans
		canCreateDep, canEditDep, canDeleteDep := user.CanCreateDeployments, user.CanEditDeployments, user.CanDeleteDeployments

		if user.Team != nil {
			canStart = canStart || user.Team.CanStart
			canStop = canStop || user.Team.CanStop
			canRestart = canRestart || user.Team.CanRestart
			canDelete = canDelete || user.Team.CanDelete
			canShell = canShell || user.Team.CanShell
			canViewHealth = canViewHealth || user.Team.CanViewSystemHealth
			canRunScans = canRunScans || user.Team.CanRunScans
			canCreateDep = canCreateDep || user.Team.CanCreateDeployments
			canEditDep = canEditDep || user.Team.CanEditDeployments
			canDeleteDep = canDeleteDep || user.Team.CanDeleteDeployments
		}

		claims := &UserClaims{
			ID:                   int(user.ID),
			Username:             username,
			IsAdmin:              user.IsAdmin,
			PasswordChanged:      user.PasswordChanged,
			CanStart:             canStart,
			CanStop:              canStop,
			CanRestart:           canRestart,
			CanDelete:            canDelete,
			CanShell:             canShell,
			CanViewSystemHealth:  canViewHealth,
			CanRunScans:          canRunScans,
			CanCreateDeployments: canCreateDep,
			CanEditDeployments:   canEditDep,
			CanDeleteDeployments: canDeleteDep,
			IsRestrictedAccess:   user.IsRestrictedAccess,
			AllowedContainers:    allowedContainers,
			IsActive:             user.IsActive,
			PasswordVersion:      user.PasswordVersion,
		}

		accessToken, refreshToken, err := issueTokenPair(claims)
		if err != nil {
			log.Printf("Failed to issue token pair: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to generate authentication tokens"})
		}

		return c.JSON(http.StatusOK, map[string]interface{}{
			"access_token":     accessToken,
			"refresh_token":    refreshToken,
			"is_admin":         user.IsAdmin,
			"password_changed": user.PasswordChanged,
		})
	})

	e.POST("/api/token/refresh", func(c echo.Context) error {
		refreshToken := strings.TrimSpace(c.FormValue("refresh_token"))
		if refreshToken == "" {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Missing refresh token"})
		}

		claims, err := validateRefreshToken(refreshToken)
		if err != nil {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid refresh token"})
		}

		var passwordChanged bool
		if err := db.DB.QueryRow(
			"SELECT password_changed FROM users WHERE id = ?",
			claims.ID,
		).Scan(&passwordChanged); err != nil {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "User verification failed"})
		}

		accessToken, newRefreshToken, err := issueTokenPair(claims)
		if err != nil {
			log.Printf("Failed to issue refreshed token pair: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to generate authentication tokens"})
		}

		return c.JSON(http.StatusOK, map[string]interface{}{
			"access_token":     accessToken,
			"refresh_token":    newRefreshToken,
			"is_admin":         claims.IsAdmin,
			"password_changed": passwordChanged,
		})
	})

	// Public configuration route
	e.GET("/api/config", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]interface{}{
			"allow_start":   CanStart,
			"allow_stop":    CanStop,
			"allow_restart": CanRestart,
			"allow_delete":  CanDelete,
			"allow_shell":   AllowShell,
			"client_access": clientAccessConfig(),
		})
	})

	// Restricted Group
	r := e.Group("/api")
	r.Use(echojwt.WithConfig(echojwt.Config{
		NewClaimsFunc: func(c echo.Context) jwt.Claims {
			return new(UserClaims)
		},
		SigningKey: SECRET_KEY,
		Skipper: func(c echo.Context) bool {
			auth := c.Request().Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer lh_pat_") {
				tokenStr := strings.TrimPrefix(auth, "Bearer ")
				var apiToken db.ApiToken
				if err := db.GormDB.Where("token = ?", tokenStr).First(&apiToken).Error; err == nil {
					db.GormDB.Model(&apiToken).Update("last_used", time.Now())
					var u db.User
					if err := db.GormDB.First(&u, apiToken.UserID).Error; err == nil {
						claims := &UserClaims{
							ID:                 int(u.ID),
							Username:           u.Username,
							IsAdmin:            u.IsAdmin,
							CanStart:           u.CanStart,
							CanStop:            u.CanStop,
							CanRestart:         u.CanRestart,
							CanDelete:          u.CanDelete,
							CanShell:           u.CanShell,
							IsRestrictedAccess: u.IsRestrictedAccess,
							AllowedContainers:  u.AllowedContainers,
							IsActive:           u.IsActive,
							PasswordChanged:    u.PasswordChanged,
							PasswordVersion:    u.PasswordVersion,
							TokenType:          "access",
						}
						mockToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
						c.Set("user", mockToken)
						return true // Skip standard JWT validation
					}
				}
			}
			return false
		},
	}))

	// Password change enforcement & session validation middleware
	r.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			token := c.Get("user").(*jwt.Token)
			claims := token.Claims.(*UserClaims)

			if claims.TokenType == tokenTypeRefresh {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid token"})
			}

			if err := refreshClaimsFromDB(claims); err != nil {
				switch errMsg := err.Error(); errMsg {
				case "account deactivated":
					return c.JSON(http.StatusForbidden, map[string]string{
						"error": "Account deactivated. Please contact administrator.",
						"code":  "ACCOUNT_DEACTIVATED",
					})
				case "session invalidated":
					return c.JSON(http.StatusUnauthorized, map[string]string{
						"error": "Session invalidated. Password was changed. Please re-login.",
						"code":  "SESSION_INVALIDATED",
					})
				case "user not found":
					return c.JSON(http.StatusUnauthorized, map[string]string{"error": "User not found."})
				default:
					// Transient DB errors or "database is locked" shouldn't log out the user
					return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Internal server error during session validation"})
				}
			}

			// Allow profile and password-change endpoints to proceed after active-state validation.
			if c.Path() == "/api/user/change-password" || c.Path() == "/api/user/me" {
				return next(c)
			}

			if !claims.PasswordChanged {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "Password change required", "code": "FORCE_PASSWORD_CHANGE"})
			}

			return next(c)
		}
	})

	RegisterImageRoutes(r, cli)
	RegisterVolumeRoutes(r, cli)
	RegisterNetworkRoutes(r, cli)
	registerApiTokenRoutes(r)
	registerMCPRoutes(r, cli)

	r.GET("/containers", func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		user := token.Claims.(*UserClaims)

		// Always check DB for current admin status (not stale JWT)
		var u db.User
		db.GormDB.Select("is_admin").First(&u, user.ID)
		dbIsAdmin := u.IsAdmin

		res, err := cli.ContainerList(context.Background(), client.ContainerListOptions{All: true, Size: true})
		if err != nil {
			log.Printf("ContainerList error: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to list containers"})
		}

		containers := extractContainers(res.Items)

		if LighthouseMode == "hub" {
			cluster.GlobalHub.RLock()
			for nodeID, spContainers := range cluster.GlobalHub.SpokeContainers {
				for _, ctr := range spContainers {
					ctr["NodeID"] = nodeID
					containers = append(containers, ctr)
				}
			}
			cluster.GlobalHub.RUnlock()
		}

		var patterns []string
		if !dbIsAdmin {
			patterns = getAuthorizedPatterns(user.ID)
		}
		log.Printf("User %d (DB Admin: %v) authorized patterns: %v", user.ID, dbIsAdmin, patterns)

		var visibleContainers []map[string]interface{}
		for _, ctr := range containers {
			name := "unknown"
			names, _ := ctr["Names"].([]interface{})
			if len(names) > 0 {
				name = names[0].(string)[1:]
			}

			image, _ := ctr["Image"].(string)

			id, ok := ctr["ID"].(string)
			if !ok {
				id, _ = ctr["Id"].(string)
			}
			if id == "" {
				continue
			}

			isPlatform := isLightHouseSelfContainer(name, image)
			if !isPlatform && isExcludedContainer(name, image) {
				continue
			}

			visible := dbIsAdmin
			if !visible {
				for _, p := range patterns {
					if matched, _ := regexp.MatchString(p, name); matched {
						visible = true
						break
					}
				}
			}

			if visible {
				ctr["_parsed_name"] = name
				ctr["_parsed_image"] = image
				ctr["_parsed_id"] = id
				ctr["_is_platform"] = isPlatform
				visibleContainers = append(visibleContainers, ctr)
			}
		}

		var list []Container
		var listMu sync.Mutex
		var wg sync.WaitGroup

		for _, ctr := range visibleContainers {
			wg.Add(1)
			go func(c map[string]interface{}) {
				defer wg.Done()

				id := c["_parsed_id"].(string)
				name := c["_parsed_name"].(string)
				image := c["_parsed_image"].(string)
				isPlatform := c["_is_platform"].(bool)

				shortID := id
				if len(id) > 12 {
					shortID = id[:12]
				}

				state, _ := c["State"].(string)
				createdVal, _ := c["Created"].(float64)
				statusVal, _ := c["Status"].(string)

				// Concurrent Inspect
				inspect, _ := cli.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{})
				cpuLimit := 0.0
				memLimit := int64(0)
				if inspect.Container.HostConfig != nil {
					if inspect.Container.HostConfig.NanoCPUs > 0 {
						cpuLimit = float64(inspect.Container.HostConfig.NanoCPUs) / 1e9
					}
					memLimit = inspect.Container.HostConfig.Memory
				}

				// Concurrent DB Fetch
				var lastCPU float64
				var lastMem float64
				var stat db.Stat
				db.GormDB.Where("container_id = ?", id).Order("timestamp DESC").First(&stat)
				lastCPU = stat.CPU
				lastMem = float64(stat.Memory)

				sizeRwVal, _ := c["SizeRw"].(int64)
				sizeRootFsVal, _ := c["SizeRootFs"].(int64)
				if sizeRwVal == 0 {
					if f, ok := c["SizeRw"].(float64); ok {
						sizeRwVal = int64(f)
					}
				}
				if sizeRootFsVal == 0 {
					if f, ok := c["SizeRootFs"].(float64); ok {
						sizeRootFsVal = int64(f)
					}
				}

				listMu.Lock()
				list = append(list, Container{
					ID:         shortID,
					Name:       name,
					Image:      image,
					State:      state,
					Created:    int64(createdVal),
					Status:     statusVal,
					CPULimit:   cpuLimit,
					MemLimit:   memLimit,
					CPU:        lastCPU,
					Memory:     int64(lastMem),
					SizeRw:     sizeRwVal,
					SizeRootFs: sizeRootFsVal,
					IsPlatform: isPlatform,
				})
				listMu.Unlock()
			}(ctr)
		}

		wg.Wait()

		return c.JSON(http.StatusOK, list)
	})

	r.GET("/containers/:id/inspect", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)

		// Always check DB for current admin status (not stale JWT)
		var u db.User
		db.GormDB.Select("is_admin").First(&u, userClaims.ID)
		dbIsAdmin := u.IsAdmin

		container, err := cli.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{Size: true})
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Container not found"})
		}

		if !dbIsAdmin {
			containerName := strings.TrimPrefix(container.Container.Name, "/")
			patterns := getAuthorizedPatterns(userClaims.ID)
			authorized := false
			for _, p := range patterns {
				if matched, _ := regexp.MatchString(p, containerName); matched {
					authorized = true
					break
				}
			}
			if !authorized {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "Access Denied"})
			}
		}

		containerName := strings.TrimPrefix(container.Container.Name, "/")
		containerImage := ""
		if container.Container.Config != nil {
			containerImage = container.Container.Config.Image
		}

		// The platform (LightHouse self) container is accessible for inspect/logs
		// but we never treat it as an excluded user container.
		if !isLightHouseSelfContainer(containerName, containerImage) && isExcludedContainer(containerName, containerImage) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Container not found"})
		}

		if container.Container.Config != nil && len(container.Container.Config.Env) > 0 {
			container.Container.Config.Env = sanitizeContainerEnv(container.Container.Config.Env)
		}

		return c.JSON(http.StatusOK, container)
	})

	r.POST("/containers/:id/action", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		action := c.FormValue("action")
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)

		if action != "start" && action != "stop" && action != "restart" && action != "remove" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid action specified."})
		}

		// Admins always bypass the env-level action gate and the per-user
		// permission check — those restrictions are for non-admin staff only.
		if !userClaims.IsAdmin && !containerActionEnvAllowed(action) {
			detail := "This action is disabled on this server."
			logAudit(userClaims.ID, userClaims.Username, action, id, "Forbidden", detail)
			return c.JSON(http.StatusForbidden, map[string]string{"error": detail})
		}

		target, err := cli.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{})
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Target container not found."})
		}
		targetImage := ""
		if target.Container.Config != nil {
			targetImage = target.Container.Config.Image
		}
		targetName := strings.TrimPrefix(target.Container.Name, "/")
		// Platform container: allow inspect/restart but block stop and remove
		if isLightHouseSelfContainer(targetName, targetImage) {
			if action == "stop" || action == "remove" {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "Cannot stop or remove the LightHouse platform container."})
			}
		} else if inspectContainerExcluded(userClaims.IsAdmin, target.Container.Name, targetImage) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Target container not found."})
		}

		if !userClaims.IsAdmin {
			can, err := staffHasContainerActionPermission(action, userClaims.ID)
			if err != nil || !can {
				detail := "This action is not permitted for this account."
				logAudit(userClaims.ID, userClaims.Username, action, id, "Forbidden", detail)
				return c.JSON(http.StatusForbidden, map[string]string{"error": detail})
			}
		}

		var u db.User
		db.GormDB.Select("is_admin").First(&u, userClaims.ID)
		dbIsAdmin := u.IsAdmin
		if !dbIsAdmin {
			patterns := getAuthorizedPatterns(userClaims.ID)
			authorized := false
			for _, p := range patterns {
				if matched, _ := regexp.MatchString(p, targetName); matched {
					authorized = true
					break
				}
			}

			if !authorized {
				logAudit(userClaims.ID, userClaims.Username, action, targetName, "Forbidden", "Security Restriction: Regex level rights missing.")
				return c.JSON(http.StatusForbidden, map[string]string{"error": "Security Restriction: You are not authorized to interact with this container resource."})
			}
		}

		ctx := context.Background()
		timeout := 60
		switch action {
		case "start":
			// Check if it belongs to a spoke
			if LighthouseMode == "hub" {
				nodeID := ""
				cluster.GlobalHub.RLock()
				for nID, spContainers := range cluster.GlobalHub.SpokeContainers {
					for _, ctr := range spContainers {
						cID, _ := ctr["ID"].(string)
						cId, _ := ctr["Id"].(string)
						if cID == id || cId == id {
							nodeID = nID
							break
						}
					}
				}
				cluster.GlobalHub.RUnlock()

				if nodeID != "" {
					err = cluster.SendCommandToSpoke(nodeID, "start", id)
				} else {
					_, err = cli.ContainerStart(ctx, id, client.ContainerStartOptions{})
				}
			} else {
				_, err = cli.ContainerStart(ctx, id, client.ContainerStartOptions{})
			}
		case "stop":
			// Check if it belongs to a spoke
			if LighthouseMode == "hub" {
				nodeID := ""
				cluster.GlobalHub.RLock()
				for nID, spContainers := range cluster.GlobalHub.SpokeContainers {
					for _, ctr := range spContainers {
						cID, _ := ctr["ID"].(string)
						cId, _ := ctr["Id"].(string)
						if cID == id || cId == id {
							nodeID = nID
							break
						}
					}
				}
				cluster.GlobalHub.RUnlock()

				if nodeID != "" {
					err = cluster.SendCommandToSpoke(nodeID, "stop", id)
				} else {
					_, err = cli.ContainerStop(ctx, id, client.ContainerStopOptions{Timeout: &timeout})
				}
			} else {
				_, err = cli.ContainerStop(ctx, id, client.ContainerStopOptions{Timeout: &timeout})
			}
		case "restart":
			_, err = cli.ContainerRestart(ctx, id, client.ContainerRestartOptions{Timeout: &timeout})
		case "remove":
			// Check if it belongs to a spoke
			if LighthouseMode == "hub" {
				nodeID := ""
				cluster.GlobalHub.RLock()
				for nID, spContainers := range cluster.GlobalHub.SpokeContainers {
					for _, ctr := range spContainers {
						cID, _ := ctr["ID"].(string)
						cId, _ := ctr["Id"].(string)
						if cID == id || cId == id {
							nodeID = nID
							break
						}
					}
				}
				cluster.GlobalHub.RUnlock()

				if nodeID != "" {
					err = cluster.SendCommandToSpoke(nodeID, "delete", id)
				} else {
					_, err = cli.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true})
				}
			} else {
				_, err = cli.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true})
			}
		}

		if err != nil {
			logAudit(userClaims.ID, userClaims.Username, action, id, "Error", "System Error: "+err.Error())
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to execute container action"})
		}

		logAudit(userClaims.ID, userClaims.Username, action, id, "Success", "Action executed successfully.")
		return c.NoContent(http.StatusOK)
	})

	r.POST("/containers/:id/scan", func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanRunScans {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Access Denied"})
		}

		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}

		if LighthouseMode == "hub" {
			nodeID := ""
			cluster.GlobalHub.RLock()
			for nID, spContainers := range cluster.GlobalHub.SpokeContainers {
				for _, ctr := range spContainers {
					cID, _ := ctr["ID"].(string)
					cId, _ := ctr["Id"].(string)
					if cID == id || cId == id {
						nodeID = nID
						break
					}
				}
			}
			cluster.GlobalHub.RUnlock()

			if nodeID != "" {
				cluster.SendCommandToSpoke(nodeID, "scan", id)
				logAudit(userClaims.ID, userClaims.Username, "SCAN", "Container:"+id, "Success", "Triggered vulnerability scan on Spoke node")
				return c.JSON(http.StatusOK, map[string]string{"status": "scanning", "message": "Scan dispatched to Spoke node"})
			}
		}

		imageParam := c.QueryParam("image")

		go func() {
			ctx := context.Background()
			ctr, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
			if err != nil {
				log.Printf("scan error: %v", err)
				return
			}
			imageName := imageParam
			if imageName == "" {
				if ctr.Container.Config != nil {
					imageName = ctr.Container.Config.Image
				}
			}
			log.Printf("Scanning image %s...", imageName)
			res, err := scanner.ScanImage(ctx, cli, imageName)
			if err != nil {
				log.Printf("scan error: %v", err)
				return
			}
			b, _ := json.Marshal(res)
			db.GormDB.Create(&db.ImageScanResult{
				Image:  imageName,
				Result: string(b),
			})

			// Check for critical vulnerabilities
			if strings.Contains(string(b), `"Severity":"CRITICAL"`) || strings.Contains(string(b), `"Severity": "CRITICAL"`) {
				alerts.Global.TriggerContainerEvent("vulnerability_found", strings.TrimPrefix(ctr.Container.Name, "/"), fmt.Sprintf("CRITICAL vulnerabilities found during scan of image: %s", imageName))
			}
			log.Printf("Scan complete for %s", imageName)
		}()

		logAudit(userClaims.ID, userClaims.Username, "SCAN", "Container:"+id, "Success", fmt.Sprintf("Triggered vulnerability scan for container %s (image: %s)", id, imageParam))
		return c.JSON(http.StatusOK, map[string]string{"status": "scanning", "message": "Scan started in background"})
	})

	r.GET("/images/scans", func(c echo.Context) error {
		imageName := c.QueryParam("image")
		if imageName == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "image required"})
		}
		var result db.ImageScanResult
		if err := db.GormDB.Where("image = ?", imageName).Order("created_at desc").First(&result).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "No scan results found"})
		}
		return c.String(http.StatusOK, result.Result)
	})

	r.GET("/containers/:id/logs/download", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)

		container, err := cli.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{})
		if err != nil {
			return c.NoContent(http.StatusNotFound)
		}
		downloadImage := ""
		if container.Container.Config != nil {
			downloadImage = container.Container.Config.Image
		}
		if inspectContainerExcluded(userClaims.IsAdmin, container.Container.Name, downloadImage) {
			return c.NoContent(http.StatusNotFound)
		}
		containerName := strings.TrimPrefix(container.Container.Name, "/")

		if !userClaims.IsAdmin {
			patterns := getAuthorizedPatterns(userClaims.ID)
			authorized := false
			for _, p := range patterns {
				if matched, _ := regexp.MatchString(p, containerName); matched {
					authorized = true
					break
				}
			}
			if !authorized {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "Permission Denied: Download restricted for this resource."})
			}
		}

		options := client.ContainerLogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Timestamps: true,
			Follow:     false,
		}

		out, err := cli.ContainerLogs(context.Background(), id, options)
		if err != nil {
			log.Printf("Failed to fetch container logs for %s: %v", id, err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch logs"})
		}
		defer out.Close()

		logAudit(userClaims.ID, userClaims.Username, "DOWNLOAD_LOGS", id, "Success", "Full log archive exported")

		c.Response().Header().Set(echo.HeaderContentDisposition, "attachment; filename="+id+"_full.log")
		c.Response().Header().Set(echo.HeaderContentType, "text/plain")
		c.Response().WriteHeader(http.StatusOK)

		_, err = io.Copy(c.Response().Writer, out)
		return err
	})

	r.GET("/containers/:id/logs", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		untilStr := c.QueryParam("until")

		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)

		container, err := cli.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{})
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Container not found"})
		}
		logsImage := ""
		if container.Container.Config != nil {
			logsImage = container.Container.Config.Image
		}
		if inspectContainerExcluded(userClaims.IsAdmin, container.Container.Name, logsImage) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Container not found"})
		}

		if !userClaims.IsAdmin {
			containerName := strings.TrimPrefix(container.Container.Name, "/")
			patterns := getAuthorizedPatterns(userClaims.ID)
			authorized := false
			for _, p := range patterns {
				if matched, _ := regexp.MatchString(p, containerName); matched {
					authorized = true
					break
				}
			}
			if !authorized {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "Access Denied"})
			}
		}

		options := client.ContainerLogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Timestamps: true,
			Follow:     false,
		}

		out, err := cli.ContainerLogs(context.Background(), id, options)
		if err != nil {
			log.Printf("Failed to fetch container logs: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch logs"})
		}
		defer out.Close()

		var output bytes.Buffer
		if container.Container.Config.Tty {
			io.Copy(&output, out)
		} else {
			stdcopy.StdCopy(&output, &output, out)
		}

		allLines := strings.Split(output.String(), "\n")
		var logs []string

		if untilStr == "" {
			// Initial load - just get last 100
			for _, line := range allLines {
				if line != "" {
					logs = append(logs, line)
				}
			}
			if len(logs) > 100 {
				logs = logs[len(logs)-100:]
			}
		} else {
			// Historical fetch - get 100 lines before 'until'
			var untilTime time.Time
			// Try parsing as RFC3339Nano (Docker's default)
			untilTime, err = time.Parse(time.RFC3339Nano, untilStr)
			if err != nil {
				// Fallback to RFC3339
				untilTime, err = time.Parse(time.RFC3339, untilStr)
				if err != nil {
					// Fallback to Unix
					if unix, err := strconv.ParseInt(untilStr, 10, 64); err == nil {
						untilTime = time.Unix(unix, 0)
					}
				}
			}

			var filtered []string
			for _, line := range allLines {
				if line == "" {
					continue
				}
				// Extract timestamp from line
				parts := strings.SplitN(line, " ", 2)
				if len(parts) > 0 {
					ts, err := time.Parse(time.RFC3339Nano, parts[0])
					if err != nil {
						ts, err = time.Parse(time.RFC3339, parts[0])
					}

					if err == nil {
						// Be inclusive (!After) to ensure no logs are missed;
						// the frontend will handle deduplication of the boundary log.
						if !ts.After(untilTime) {
							filtered = append(filtered, line)
						}
					}
				}
			}

			if len(filtered) > 100 {
				logs = filtered[len(filtered)-100:]
			} else {
				logs = filtered
			}
		}

		log.Printf("[API] Found %d lines for %s (until: %s)", len(logs), id, untilStr)
		return c.JSON(http.StatusOK, logs)
	})

	r.GET("/containers/:id/logs/count", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)

		container, err := cli.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{})
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Container not found"})
		}
		countImage := ""
		if container.Container.Config != nil {
			countImage = container.Container.Config.Image
		}
		if inspectContainerExcluded(userClaims.IsAdmin, container.Container.Name, countImage) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Container not found"})
		}

		if !userClaims.IsAdmin {
			containerName := strings.TrimPrefix(container.Container.Name, "/")
			patterns := getAuthorizedPatterns(userClaims.ID)
			authorized := false
			for _, p := range patterns {
				if matched, _ := regexp.MatchString(p, containerName); matched {
					authorized = true
					break
				}
			}
			if !authorized {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "Access Denied"})
			}
		}

		options := client.ContainerLogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     false,
		}

		out, err := cli.ContainerLogs(context.Background(), id, options)
		if err != nil {
			log.Printf("Failed to fetch container logs: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch logs"})
		}
		defer out.Close()

		var output bytes.Buffer
		if container.Container.Config.Tty {
			io.Copy(&output, out)
		} else {
			stdcopy.StdCopy(&output, &output, out)
		}

		count := strings.Count(output.String(), "\n")
		return c.JSON(http.StatusOK, map[string]int{"total": count})
	})

	r.GET("/containers/:id/stats", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)

		container, err := cli.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{})
		if err != nil {
			return c.NoContent(http.StatusNotFound)
		}
		statsImage := ""
		if container.Container.Config != nil {
			statsImage = container.Container.Config.Image
		}
		if inspectContainerExcluded(userClaims.IsAdmin, container.Container.Name, statsImage) {
			return c.NoContent(http.StatusNotFound)
		}
		containerName := strings.TrimPrefix(container.Container.Name, "/")

		if !userClaims.IsAdmin {
			patterns := getAuthorizedPatterns(userClaims.ID)
			authorized := false
			for _, p := range patterns {
				if matched, _ := regexp.MatchString(p, containerName); matched {
					authorized = true
					break
				}
			}
			if !authorized {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "Unauthorized resource access."})
			}
		}

		stats, err := cli.ContainerStats(context.Background(), id, client.ContainerStatsOptions{Stream: true})
		if err != nil {
			log.Printf("ContainerStats error: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch container stats"})
		}
		defer stats.Body.Close()

		c.Response().Header().Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		c.Response().WriteHeader(http.StatusOK)

		enc := json.NewEncoder(c.Response())
		dec := json.NewDecoder(stats.Body)
		for {
			var data interface{}
			if err := dec.Decode(&data); err != nil {
				break
			}
			if err := enc.Encode(data); err != nil {
				break
			}
			c.Response().Flush()
		}
		return nil
	})

	r.GET("/containers/:id/stats-now", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)

		container, err := cli.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{})
		if err != nil {
			return c.NoContent(http.StatusNotFound)
		}
		nowImage := ""
		if container.Container.Config != nil {
			nowImage = container.Container.Config.Image
		}
		if inspectContainerExcluded(userClaims.IsAdmin, container.Container.Name, nowImage) {
			return c.NoContent(http.StatusNotFound)
		}
		containerName := strings.TrimPrefix(container.Container.Name, "/")

		if !userClaims.IsAdmin {
			patterns := getAuthorizedPatterns(userClaims.ID)
			authorized := false
			for _, p := range patterns {
				if matched, _ := regexp.MatchString(p, containerName); matched {
					authorized = true
					break
				}
			}
			if !authorized {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "Unauthorized resource access."})
			}
		}

		// To get accurate CPU %, we need two samples.
		// We'll take a quick 200ms sample to stay responsive.
		s1, err := cli.ContainerStats(context.Background(), id, client.ContainerStatsOptions{Stream: false})
		if err != nil {
			log.Printf("ContainerStats error (sample 1): %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch container stats"})
		}
		var v1 struct {
			CPUStats struct {
				CPUUsage struct {
					TotalUsage uint64 `json:"total_usage"`
				} `json:"cpu_usage"`
				SystemUsage uint64 `json:"system_cpu_usage"`
			} `json:"cpu_stats"`
			Networks map[string]struct {
				RxBytes uint64 `json:"rx_bytes"`
				TxBytes uint64 `json:"tx_bytes"`
			} `json:"networks"`
			BlkioStats struct {
				IoServiceBytesRecursive []struct {
					Op    string `json:"op"`
					Value uint64 `json:"value"`
				} `json:"io_service_bytes_recursive"`
			} `json:"blkio_stats"`
		}
		json.NewDecoder(s1.Body).Decode(&v1)
		s1.Body.Close()

		time.Sleep(500 * time.Millisecond)

		s2, err := cli.ContainerStats(context.Background(), id, client.ContainerStatsOptions{Stream: false})
		if err != nil {
			log.Printf("ContainerStats error (sample 2): %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch container stats"})
		}
		defer s2.Body.Close()

		var v2 struct {
			CPUStats struct {
				CPUUsage struct {
					TotalUsage uint64 `json:"total_usage"`
				} `json:"cpu_usage"`
				SystemUsage uint64 `json:"system_cpu_usage"`
				OnlineCPUs  uint32 `json:"online_cpus"`
			} `json:"cpu_stats"`
			MemoryStats struct {
				Usage uint64            `json:"usage"`
				Stats map[string]uint64 `json:"stats"`
			} `json:"memory_stats"`
			Networks map[string]struct {
				RxBytes uint64 `json:"rx_bytes"`
				TxBytes uint64 `json:"tx_bytes"`
			} `json:"networks"`
			BlkioStats struct {
				IoServiceBytesRecursive []struct {
					Op    string `json:"op"`
					Value uint64 `json:"value"`
				} `json:"io_service_bytes_recursive"`
			} `json:"blkio_stats"`
			PidsStats struct {
				Current uint64 `json:"current"`
			} `json:"pids_stats"`
		}
		if err := json.NewDecoder(s2.Body).Decode(&v2); err != nil {
			log.Printf("Failed to decode container stats: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to parse container stats"})
		}

		cpuDelta := float64(v2.CPUStats.CPUUsage.TotalUsage) - float64(v1.CPUStats.CPUUsage.TotalUsage)
		systemDelta := float64(v2.CPUStats.SystemUsage) - float64(v1.CPUStats.SystemUsage)

		onlineCPUs := float64(v2.CPUStats.OnlineCPUs)
		if onlineCPUs == 0 {
			onlineCPUs = float64(runtime.NumCPU())
		}

		cpuPercent := 0.0
		if systemDelta > 0 && cpuDelta > 0 {
			cpuPercent = (cpuDelta / systemDelta) * onlineCPUs * 100.0
		}
		// cgroups v2 uses "inactive_file", cgroups v1 uses "cache".
		// Docker recommends subtracting inactive_file for accurate working-set memory.
		memUsed := v2.MemoryStats.Usage
		if inactiveFile, ok := v2.MemoryStats.Stats["inactive_file"]; ok && inactiveFile < memUsed {
			memUsed -= inactiveFile
		} else if cache, ok := v2.MemoryStats.Stats["cache"]; ok && cache < memUsed {
			memUsed -= cache
		}

		var rx1, tx1, r1, w1 uint64
		for _, netIf := range v1.Networks {
			rx1 += netIf.RxBytes
			tx1 += netIf.TxBytes
		}
		for _, io := range v1.BlkioStats.IoServiceBytesRecursive {
			switch op := strings.ToLower(io.Op); op {
			case "read":
				r1 += io.Value
			case "write":
				w1 += io.Value
			}
		}

		var rx2, tx2, r2, w2 uint64
		for _, netIf := range v2.Networks {
			rx2 += netIf.RxBytes
			tx2 += netIf.TxBytes
		}
		for _, io := range v2.BlkioStats.IoServiceBytesRecursive {
			switch op := strings.ToLower(io.Op); op {
			case "read":
				r2 += io.Value
			case "write":
				w2 += io.Value
			}
		}

		// Calculate rate per second (approx) from 500ms diff
		rxRate := (rx2 - rx1) * 2
		txRate := (tx2 - tx1) * 2
		readRate := (r2 - r1) * 2
		writeRate := (w2 - w1) * 2

		return c.JSON(http.StatusOK, map[string]interface{}{
			"cpu":        cpuPercent,
			"memory":     memUsed,
			"net_rx":     rxRate,
			"net_tx":     txRate,
			"disk_read":  readRate,
			"disk_write": writeRate,
			"pids":       v2.PidsStats.Current,
		})
	})

	r.GET("/containers/:id/history", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)

		container, err := cli.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{})
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Container not found"})
		}
		historyImage := ""
		if container.Container.Config != nil {
			historyImage = container.Container.Config.Image
		}
		if inspectContainerExcluded(userClaims.IsAdmin, container.Container.Name, historyImage) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Container not found"})
		}

		longID := container.Container.ID
		if longID == "" {
			longID = id
		}

		if !userClaims.IsAdmin {
			containerName := strings.TrimPrefix(container.Container.Name, "/")

			patterns := getAuthorizedPatterns(userClaims.ID)
			authorized := false
			for _, p := range patterns {
				if matched, _ := regexp.MatchString(p, containerName); matched {
					authorized = true
					break
				}
			}
			if !authorized {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "Unauthorized resource access."})
			}
		}

		duration := c.QueryParam("duration")
		from := c.QueryParam("from")
		to := c.QueryParam("to")

		var stats []db.Stat
		query := db.GormDB.Where("container_id = ?", longID)

		if from != "" && to != "" {
			query = query.Where("timestamp BETWEEN ? AND ?", from, to)
		} else if duration != "" && strings.HasSuffix(duration, "h") {
			hours, err := strconv.Atoi(strings.TrimSuffix(duration, "h"))
			if err == nil && hours > 0 {
				query = query.Where("timestamp >= datetime('now', '-" + strconv.Itoa(hours) + " hours')")
			}
		}

		query.Order("timestamp DESC").Limit(200).Find(&stats)

		var results []map[string]interface{}
		for _, s := range stats {
			results = append(results, map[string]interface{}{
				"cpu":        s.CPU,
				"memory":     s.Memory,
				"net_rx":     s.NetRxBytes,
				"net_tx":     s.NetTxBytes,
				"disk_read":  s.DiskReadBytes,
				"disk_write": s.DiskWriteBytes,
				"timestamp":  s.Timestamp,
			})
		}
		return c.JSON(http.StatusOK, results)
	})

	r.GET("/system/storage", func(c echo.Context) error {
		var stat syscall.Statfs_t
		err := syscall.Statfs("/", &stat)
		if err != nil {
			log.Printf("Failed to get storage stats: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to query storage"})
		}

		totalSize := int64(stat.Blocks) * int64(stat.Bsize)
		freeSize := int64(stat.Bavail) * int64(stat.Bsize)
		usedSize := totalSize - freeSize

		return c.JSON(http.StatusOK, map[string]interface{}{
			"total_bytes": totalSize,
			"used_bytes":  usedSize,
		})
	})

	r.GET("/system/history", func(c echo.Context) error {
		duration := c.QueryParam("duration")
		from := c.QueryParam("from")
		to := c.QueryParam("to")
		daysStr := c.QueryParam("days")

		var systemStats []db.SystemStat
		query := db.GormDB

		if from != "" && to != "" {
			query = query.Where("timestamp BETWEEN ? AND ?", from, to)
		} else if duration != "" && strings.HasSuffix(duration, "h") {
			hours, err := strconv.Atoi(strings.TrimSuffix(duration, "h"))
			if err == nil && hours > 0 {
				query = query.Where("timestamp >= datetime('now', '-" + strconv.Itoa(hours) + " hours')")
			}
		} else {
			days := 30
			if d, err := strconv.Atoi(daysStr); err == nil {
				days = d
			}
			query = query.Where("timestamp > ?", time.Now().AddDate(0, 0, -days))
		}

		err := query.Order("timestamp DESC").Find(&systemStats).Error
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to query DB"})
		}

		var history []map[string]interface{}
		for _, stat := range systemStats {
			history = append(history, map[string]interface{}{
				"cpu":        stat.CPU,
				"memory":     stat.Memory,
				"net_rx":     stat.NetRxBytes,
				"net_tx":     stat.NetTxBytes,
				"disk_read":  stat.DiskReadBytes,
				"disk_write": stat.DiskWriteBytes,
				"timestamp":  stat.Timestamp,
			})
		}
		return c.JSON(http.StatusOK, history)
	})

	r.GET("/system/stats", func(c echo.Context) error {
		sysStatsMu.RLock()
		defer sysStatsMu.RUnlock()
		if latestSystemStats == nil {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "Stats not ready"})
		}
		return c.JSON(http.StatusOK, latestSystemStats)
	})

	r.GET("/system/info", func(c echo.Context) error {
		dockerVer := "Unknown"
		if v, err := cli.ServerVersion(context.Background(), client.ServerVersionOptions{}); err == nil {
			dockerVer = v.Version
		}

		composeVer := "Unknown"
		out, err := exec.Command("docker", "compose", "version", "--short").Output()
		if err == nil {
			composeVer = strings.TrimSpace(string(out))
		} else {
			out, err = exec.Command("docker-compose", "version", "--short").Output()
			if err == nil {
				composeVer = strings.TrimSpace(string(out))
			}
		}

		return c.JSON(http.StatusOK, map[string]interface{}{
			"docker_version":  dockerVer,
			"compose_version": composeVer,
		})
	})

	r.POST("/user/change-password", func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		newPassword := c.FormValue("password")

		if len(newPassword) < minPasswordLength {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("Password must be at least %d characters", minPasswordLength)})
		}
		if len(newPassword) > maxPasswordLength {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("Password must not exceed %d characters", maxPasswordLength)})
		}

		var user db.User
		if err := db.GormDB.First(&user, userClaims.ID).Error; err != nil {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "User not found"})
		}
		if !user.IsActive {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Account has been deactivated"})
		}

		if user.PasswordChanged {
			currentPassword := c.FormValue("current_password")
			if currentPassword == "" {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "Current password is required"})
			}
			if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(currentPassword)) != nil {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Current password is incorrect"})
			}
		}

		h, _ := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
		db.GormDB.Model(&user).Updates(map[string]interface{}{
			"password":         string(h),
			"password_changed": true,
			"password_version": gorm.Expr("COALESCE(password_version, 1) + 1"),
		})

		log.Printf("Password successfully updated for user %d", user.ID)
		return c.NoContent(http.StatusOK)
	})

	r.GET("/user/me", func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		claims := token.Claims.(*UserClaims)
		var dbUser db.User
		err := db.GormDB.Preload("Team").First(&dbUser, claims.ID).Error
		if err != nil {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "User not found"})
		}
		if !dbUser.IsActive {
			return c.JSON(http.StatusForbidden, map[string]string{
				"error": "Account deactivated",
				"code":  "ACCOUNT_DEACTIVATED",
			})
		}

		response := map[string]interface{}{
			"id":                     dbUser.ID,
			"username":               dbUser.Username,
			"is_admin":               dbUser.IsAdmin,
			"can_start":              dbUser.CanStart || (dbUser.Team != nil && dbUser.Team.CanStart),
			"can_stop":               dbUser.CanStop || (dbUser.Team != nil && dbUser.Team.CanStop),
			"can_restart":            dbUser.CanRestart || (dbUser.Team != nil && dbUser.Team.CanRestart),
			"can_delete":             dbUser.CanDelete || (dbUser.Team != nil && dbUser.Team.CanDelete),
			"can_shell":              dbUser.CanShell || (dbUser.Team != nil && dbUser.Team.CanShell),
			"can_view_system_health": dbUser.CanViewSystemHealth || (dbUser.Team != nil && dbUser.Team.CanViewSystemHealth),
			"can_run_scans":          dbUser.CanRunScans || (dbUser.Team != nil && dbUser.Team.CanRunScans),
			"can_create_deployments": dbUser.CanCreateDeployments || (dbUser.Team != nil && dbUser.Team.CanCreateDeployments),
			"can_edit_deployments":   dbUser.CanEditDeployments || (dbUser.Team != nil && dbUser.Team.CanEditDeployments),
			"can_delete_deployments": dbUser.CanDeleteDeployments || (dbUser.Team != nil && dbUser.Team.CanDeleteDeployments),
			"is_restricted_access":   dbUser.IsRestrictedAccess || (dbUser.Team != nil),
			"allowed_containers":     dbUser.AllowedContainers, // this is raw, frontend doesn't need to merge it for UI display
			"password_changed":       dbUser.PasswordChanged,
			"is_active":              dbUser.IsActive,
			"team_id":                dbUser.TeamID,
		}

		return c.JSON(http.StatusOK, response)
	})

	// Admin Only Routes
	admin := r.Group("/admin")
	admin.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			token := c.Get("user").(*jwt.Token)
			user := token.Claims.(*UserClaims)
			var isAdmin bool
			err := db.GormDB.Raw("SELECT is_admin FROM users WHERE id = ? AND is_active = 1", user.ID).Scan(&isAdmin).Error
			if err != nil || !isAdmin {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "Admin access required"})
			}
			user.IsAdmin = isAdmin
			return next(c)
		}
	})
	admin.GET("/teams", func(c echo.Context) error {
		var teams []db.Team
		if err := db.GormDB.Find(&teams).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch teams"})
		}
		return c.JSON(http.StatusOK, teams)
	})

	admin.POST("/teams", func(c echo.Context) error {
		name := c.FormValue("name")
		description := c.FormValue("description")
		allowedContainers := c.FormValue("allowed_containers")
		if allowedContainers == "" {
			allowedContainers = ".*"
		}
		if name == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Team name is required"})
		}

		team := db.Team{
			Name: name, Description: description, AllowedContainers: allowedContainers,
			CanStart:             c.FormValue("can_start") == "true",
			CanStop:              c.FormValue("can_stop") == "true",
			CanRestart:           c.FormValue("can_restart") == "true",
			CanDelete:            c.FormValue("can_delete") == "true",
			CanShell:             c.FormValue("can_shell") == "true",
			CanViewSystemHealth:  c.FormValue("can_view_system_health") == "true",
			CanRunScans:          c.FormValue("can_run_scans") == "true",
			CanCreateDeployments: c.FormValue("can_create_deployments") == "true",
			CanEditDeployments:   c.FormValue("can_edit_deployments") == "true",
			CanDeleteDeployments: c.FormValue("can_delete_deployments") == "true",
			AlertsEmailAddress:   c.FormValue("alerts_email_address"),
			SlackWebhookUrl:      c.FormValue("slack_webhook_url"),
			MSTeamsWebhookUrl:    c.FormValue("msteams_webhook_url"),
			GChatWebhookUrl:      c.FormValue("gchat_webhook_url"),
			GenericWebhookUrl:    c.FormValue("generic_webhook_url"),
		}

		roleTmpl := c.FormValue("role_template_id")
		if roleTmpl != "" && roleTmpl != "null" {
			idUint, err := strconv.ParseUint(roleTmpl, 10, 32)
			if err == nil {
				parsedID := uint(idUint)
				team.RoleTemplateID = &parsedID
			}
		}

		if err := db.GormDB.Create(&team).Error; err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Failed to create team, maybe name already exists"})
		}
		return c.NoContent(http.StatusCreated)
	})

	admin.PUT("/teams/:id", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		name := c.FormValue("name")
		description := c.FormValue("description")
		allowedContainers := c.FormValue("allowed_containers")
		if allowedContainers == "" {
			allowedContainers = ".*"
		}

		updates := map[string]interface{}{
			"name": name, "description": description, "allowed_containers": allowedContainers,
			"can_start":              c.FormValue("can_start") == "true",
			"can_stop":               c.FormValue("can_stop") == "true",
			"can_restart":            c.FormValue("can_restart") == "true",
			"can_delete":             c.FormValue("can_delete") == "true",
			"can_shell":              c.FormValue("can_shell") == "true",
			"can_view_system_health": c.FormValue("can_view_system_health") == "true",
			"can_run_scans":          c.FormValue("can_run_scans") == "true",
			"can_create_deployments": c.FormValue("can_create_deployments") == "true",
			"can_edit_deployments":   c.FormValue("can_edit_deployments") == "true",
			"can_delete_deployments": c.FormValue("can_delete_deployments") == "true",
			"alerts_email_address":   c.FormValue("alerts_email_address"),
			"slack_webhook_url":      c.FormValue("slack_webhook_url"),
			"ms_teams_webhook_url":   c.FormValue("msteams_webhook_url"),
			"g_chat_webhook_url":     c.FormValue("gchat_webhook_url"),
			"generic_webhook_url":    c.FormValue("generic_webhook_url"),
			"role_template_id":       nil,
		}

		roleTmpl := c.FormValue("role_template_id")
		if roleTmpl != "" && roleTmpl != "null" {
			idUint, err := strconv.ParseUint(roleTmpl, 10, 32)
			if err == nil {
				parsedID := uint(idUint)
				updates["role_template_id"] = &parsedID
			}
		}

		db.GormDB.Model(&db.Team{}).Where("id = ?", id).Updates(updates)
		return c.NoContent(http.StatusOK)
	})

	admin.DELETE("/teams/:id", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		db.GormDB.Where("id = ?", id).Delete(&db.Team{})
		return c.NoContent(http.StatusOK)
	})

	admin.GET("/users", func(c echo.Context) error {
		page, _ := strconv.Atoi(c.QueryParam("page"))
		if page < 1 {
			page = 1
		}
		limit := 10
		offset := (page - 1) * limit

		var total int64
		db.GormDB.Model(&db.User{}).Count(&total)

		var dbUsers []db.User
		if err := db.GormDB.Preload("Team").Limit(limit).Offset(offset).Find(&dbUsers).Error; err != nil {
			log.Printf("Failed to query users: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch users"})
		}

		users := make([]map[string]interface{}, 0, len(dbUsers))
		for _, u := range dbUsers {
			users = append(users, map[string]interface{}{
				"id":                     u.ID,
				"username":               u.Username,
				"is_admin":               u.IsAdmin,
				"can_start":              u.CanStart || (u.Team != nil && u.Team.CanStart),
				"can_stop":               u.CanStop || (u.Team != nil && u.Team.CanStop),
				"can_restart":            u.CanRestart || (u.Team != nil && u.Team.CanRestart),
				"can_delete":             u.CanDelete || (u.Team != nil && u.Team.CanDelete),
				"can_shell":              u.CanShell || (u.Team != nil && u.Team.CanShell),
				"can_view_system_health": u.CanViewSystemHealth || (u.Team != nil && u.Team.CanViewSystemHealth),
				"can_run_scans":          u.CanRunScans || (u.Team != nil && u.Team.CanRunScans),
				"can_create_deployments": u.CanCreateDeployments || (u.Team != nil && u.Team.CanCreateDeployments),
				"can_edit_deployments":   u.CanEditDeployments || (u.Team != nil && u.Team.CanEditDeployments),
				"can_delete_deployments": u.CanDeleteDeployments || (u.Team != nil && u.Team.CanDeleteDeployments),
				"is_restricted_access":   u.IsRestrictedAccess || (u.Team != nil),
				"allowed_containers":     u.AllowedContainers,
				"is_active":              u.IsActive,
				"team_id":                u.TeamID,
			})
		}
		return c.JSON(http.StatusOK, map[string]interface{}{
			"users": users,
			"total": total,
			"page":  page,
			"pages": (int(total) + limit - 1) / limit,
		})
	})

	admin.PUT("/users/:id/active", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		isActive := c.FormValue("is_active") == "true"
		db.GormDB.Model(&db.User{}).Where("id = ? AND id != 1", id).Update("is_active", isActive)

		token := c.Get("user").(*jwt.Token)
		claims := token.Claims.(*UserClaims)
		status := "Disabled"
		if isActive {
			status = "Enabled"
		}
		logAudit(claims.ID, claims.Username, "UPDATE_USER_STATUS", "User:"+id, "Success", "Administrator changed user status to: "+status)

		return c.NoContent(http.StatusOK)
	})

	admin.POST("/users", func(c echo.Context) error {
		authMethod := c.FormValue("authMethod")
		roleTemplateID := c.FormValue("role_template_id")

		teamIDStr := c.FormValue("team_id")
		var teamID *uint
		if teamIDStr != "" {
			if id, err := strconv.ParseUint(teamIDStr, 10, 32); err == nil {
				tid := uint(id)
				teamID = &tid
			}
		}

		isAdminValue := c.FormValue("is_admin") == "true"

		if !isAdminValue && teamID == nil && (roleTemplateID == "" || authMethod == "") {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Role Template and Auth Method are required"})
		}
		if isAdminValue && authMethod == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Auth Method is required"})
		}

		// Load role template if team is not assigned and not an admin
		var rt db.RoleTemplate
		var rtID *uint
		if teamID == nil && !isAdminValue {
			if err := db.GormDB.First(&rt, roleTemplateID).Error; err != nil {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid Role Template"})
			}
			rtID = func(i uint) *uint { return &i }(rt.ID)
		}

		isRestricted := c.FormValue("is_restricted") == "true"
		allowedContainers := c.FormValue("allowed_containers")
		if allowedContainers == "" {
			allowedContainers = ".*"
		}

		switch authMethod {
		case "local":
			username := c.FormValue("username")
			password := c.FormValue("password")
			if username == "" || password == "" {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "Username and Password required for local auth"})
			}
			if len(password) > maxPasswordLength {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("Password must not exceed %d characters", maxPasswordLength)})
			}

			h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Could not hash password"})
			}

			user := db.User{
				Username: username, Password: string(h), Email: username + "@local", RoleTemplateID: rtID,
				CanStart: rt.CanStart, CanStop: rt.CanStop, CanRestart: rt.CanRestart, CanDelete: rt.CanDelete, CanShell: rt.CanShell,
				CanViewSystemHealth: rt.CanViewSystemHealth, CanRunScans: rt.CanRunScans,
				CanCreateDeployments: rt.CanCreateDeployments, CanEditDeployments: rt.CanEditDeployments, CanDeleteDeployments: rt.CanDeleteDeployments,
				IsRestrictedAccess: isRestricted, AllowedContainers: allowedContainers, TeamID: teamID,
				PasswordChanged: false, IsActive: true, IsAdmin: c.FormValue("is_admin") == "true",
			}
			if err := db.GormDB.Create(&user).Error; err != nil {
				log.Printf("Failed to create local user: %v", err)
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "Failed to create user. Username may already exist."})
			}

			token := c.Get("user").(*jwt.Token)
			claims := token.Claims.(*UserClaims)
			logAudit(claims.ID, claims.Username, "CREATE_USER", "User:"+user.Username, "Success", "Administrator created local user: "+user.Username)

			return c.NoContent(http.StatusCreated)
		case "invite":
			email := c.FormValue("email")
			if email == "" {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "Email is required for invite auth"})
			}

			// Generate invite token
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Could not generate token"})
			}
			inviteToken := hex.EncodeToString(b)
			expiresAt := time.Now().Add(24 * time.Hour)

			user := db.User{
				Username: email, Email: email, InviteToken: inviteToken, InviteExpiresAt: &expiresAt,
				RoleTemplateID: rtID,
				CanStart:       rt.CanStart, CanStop: rt.CanStop, CanRestart: rt.CanRestart, CanDelete: rt.CanDelete, CanShell: rt.CanShell,
				CanViewSystemHealth: rt.CanViewSystemHealth, CanRunScans: rt.CanRunScans,
				CanCreateDeployments: rt.CanCreateDeployments, CanEditDeployments: rt.CanEditDeployments, CanDeleteDeployments: rt.CanDeleteDeployments,
				IsRestrictedAccess: isRestricted, AllowedContainers: allowedContainers, TeamID: teamID,
				PasswordChanged: false, IsActive: true, IsAdmin: c.FormValue("is_admin") == "true",
			}
			if err := db.GormDB.Create(&user).Error; err != nil {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "User already exists"})
			}

			// Send email via SMTP
			var smtpHost, smtpUser, smtpPass string
			var smtpPort int
			var settings db.Setting
			db.GormDB.First(&settings, 1).Scan(&settings)
			smtpHost, smtpPort, smtpUser, smtpPass = settings.SmtpHost, settings.SmtpPort, settings.SmtpUser, settings.SmtpPass

			if smtpHost != "" {
				auth := smtp.PlainAuth("", smtpUser, smtpPass, smtpHost)
				inviteURL := fmt.Sprintf("%s://%s/auth/google?invite_token=%s", requestScheme(c.Request()), c.Request().Host, inviteToken)

				body := fmt.Sprintf(`<html>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; background-color: #f8fafc; padding: 40px 20px; margin: 0;">
  <div style="max-width: 500px; margin: 0 auto; background: #ffffff; padding: 32px; border-radius: 12px; box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.1), 0 2px 4px -1px rgba(0, 0, 0, 0.06); text-align: center;">
    <h2 style="color: #0f172a; margin-top: 0; font-size: 24px;">Welcome to LightHouse</h2>
    <p style="color: #475569; font-size: 16px; line-height: 1.6; margin-bottom: 32px;">
      You have been invited to join <strong>LightHouse</strong>, your comprehensive container management platform. Click the button below to accept your invitation and get started.
    </p>
    <a href="%s" style="background-color: #1554D2; color: #ffffff; text-decoration: none; padding: 14px 28px; border-radius: 8px; font-weight: 600; font-size: 16px; display: inline-block; transition: background-color 0.2s;">
      Accept Invitation
    </a>
    <div style="margin-top: 32px; padding-top: 24px; border-top: 1px solid #e2e8f0; text-align: left;">
      <p style="color: #64748b; font-size: 13px; margin: 0; line-height: 1.5;">
        If the button doesn't work, copy and paste this link into your browser:<br>
        <a href="%s" style="color: #1554D2; word-break: break-all; margin-top: 4px; display: inline-block;">%s</a>
      </p>
    </div>
  </div>
</body>
</html>`, inviteURL, inviteURL, inviteURL)

				msg := []byte(fmt.Sprintf("To: %s\r\nSubject: You've been invited to LightHouse\r\nMIME-version: 1.0;\r\nContent-Type: text/html; charset=\"UTF-8\";\r\n\r\n%s", email, body))
				err = smtp.SendMail(fmt.Sprintf("%s:%d", smtpHost, smtpPort), auth, smtpUser, []string{email}, msg)
				if err != nil {
					log.Printf("Failed to send invite email: %v", err)
				}
			}

			token := c.Get("user").(*jwt.Token)
			claims := token.Claims.(*UserClaims)
			logAudit(claims.ID, claims.Username, "CREATE_USER_INVITE", "User:"+email, "Success", "Administrator invited user: "+email)

			return c.NoContent(http.StatusCreated)
		default:
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid auth method"})
		}
	})

	admin.PUT("/users/:id/permissions", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		canStart, canStop, canRestart, canDelete, canShell := clampStaffActionPermissions(
			c.FormValue("can_start") == "true",
			c.FormValue("can_stop") == "true",
			c.FormValue("can_restart") == "true",
			c.FormValue("can_delete") == "true",
			c.FormValue("can_shell") == "true",
		)
		isRestricted := c.FormValue("is_restricted_access") == "true"
		allowedContainers := c.FormValue("allowed_containers")
		teamIDStr := c.FormValue("team_id")
		var teamID *uint
		if teamIDStr != "" {
			if id, err := strconv.ParseUint(teamIDStr, 10, 32); err == nil {
				tid := uint(id)
				teamID = &tid
			}
		}

		db.GormDB.Model(&db.User{}).Where("id = ?", id).Updates(map[string]interface{}{
			"can_start": canStart, "can_stop": canStop, "can_restart": canRestart,
			"can_delete": canDelete, "can_shell": canShell, "is_restricted_access": isRestricted,
			"can_view_system_health": c.FormValue("can_view_system_health") == "true",
			"can_run_scans":          c.FormValue("can_run_scans") == "true",
			"can_create_deployments": c.FormValue("can_create_deployments") == "true",
			"can_edit_deployments":   c.FormValue("can_edit_deployments") == "true",
			"can_delete_deployments": c.FormValue("can_delete_deployments") == "true",
			"allowed_containers":     allowedContainers, "team_id": teamID,
		})
		// If teamIDStr is explicitly "null", we should set team_id to null
		if teamIDStr == "null" {
			db.GormDB.Model(&db.User{}).Where("id = ?", id).Update("team_id", nil)
		}

		token := c.Get("user").(*jwt.Token)
		claims := token.Claims.(*UserClaims)
		logAudit(claims.ID, claims.Username, "UPDATE_USER_PERMISSIONS", "User:"+id, "Success", "Administrator updated user permissions")

		return c.NoContent(http.StatusOK)
	})

	admin.PUT("/users/:id/password", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		newPassword := c.FormValue("password")
		if !isPasswordStrongEnough(newPassword) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("Password must be at least %d characters", minPasswordLength)})
		}
		if len(newPassword) > maxPasswordLength {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("Password must not exceed %d characters", maxPasswordLength)})
		}

		h, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Password is too long or invalid. Please use a shorter password."})
		}
		db.GormDB.Model(&db.User{}).Where("id = ?", id).Updates(map[string]interface{}{
			"password": string(h), "password_changed": true, "password_version": gorm.Expr("COALESCE(password_version, 1) + 1"),
		})

		token := c.Get("user").(*jwt.Token)
		claims := token.Claims.(*UserClaims)
		logAudit(claims.ID, claims.Username, "RESET_PASSWORD", "User:"+id, "Success", "Administrator reset user password")

		return c.NoContent(http.StatusOK)
	})

	admin.DELETE("/users/:id", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		res := db.GormDB.Where("id = ? AND id != 1", id).Delete(&db.User{})
		if res.Error != nil {
			log.Printf("Failed to delete user %s: %v", id, res.Error)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to delete user"})
		}
		if res.RowsAffected == 0 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Cannot delete primary admin user or user not found"})
		}

		token := c.Get("user").(*jwt.Token)
		claims := token.Claims.(*UserClaims)
		logAudit(claims.ID, claims.Username, "DELETE_USER", "User:"+id, "Success", "Administrator deleted user")

		return c.JSON(http.StatusOK, map[string]string{"message": "deleted"})
	})

	admin.GET("/audit", func(c echo.Context) error {
		from := c.QueryParam("from")
		to := c.QueryParam("to")

		query := db.GormDB.Model(&db.AuditLog{})
		if from != "" && to != "" {
			query = query.Where("timestamp BETWEEN ? AND ?", from, to)
		}

		var logs []db.AuditLog
		query.Order("timestamp DESC").Limit(1000).Find(&logs)

		logsList := make([]map[string]interface{}, 0)
		for _, l := range logs {
			logsList = append(logsList, map[string]interface{}{
				"id":        l.ID,
				"user_id":   l.UserID,
				"username":  l.Username,
				"action":    l.Action,
				"resource":  l.Resource,
				"status":    l.Status,
				"message":   l.Message,
				"timestamp": l.Timestamp,
			})
		}
		return c.JSON(http.StatusOK, logsList)
	})

	// ── Alert Rules CRUD (admin-only, under /api/admin/alerts) ────────────────

	// LIST all rules
	admin.GET("/role_templates", func(c echo.Context) error {
		var templates []db.RoleTemplate
		db.GormDB.Find(&templates)

		var res []map[string]interface{}
		for _, t := range templates {
			res = append(res, map[string]interface{}{
				"id":                   t.ID,
				"name":                 t.Name,
				"can_start":            t.CanStart,
				"can_stop":             t.CanStop,
				"can_restart":          t.CanRestart,
				"can_delete":           t.CanDelete,
				"can_shell":            t.CanShell,
				"is_restricted_access": t.IsRestrictedAccess,
				"allowed_containers":   t.AllowedContainers,
			})
		}
		return c.JSON(http.StatusOK, res)
	})

	admin.POST("/role_templates", func(c echo.Context) error {
		var rt db.RoleTemplate
		if err := c.Bind(&rt); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid payload"})
		}
		if rt.AllowedContainers == "" {
			rt.AllowedContainers = ".*"
		}
		if err := db.GormDB.Create(&rt).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create role template"})
		}
		return c.NoContent(http.StatusCreated)
	})

	admin.DELETE("/role_templates/:id", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		db.GormDB.Delete(&db.RoleTemplate{}, id)
		return c.NoContent(http.StatusOK)
	})

	admin.GET("/alerts/rules", func(c echo.Context) error {
		var rules []alerts.AlertRule
		db.GormDB.Order("id DESC").Find(&rules)
		return c.JSON(http.StatusOK, rules)
	})

	// GET single rule
	admin.GET("/settings", func(c echo.Context) error {
		var settings db.Setting
		db.GormDB.FirstOrCreate(&settings, db.Setting{ID: 1})

		maskSecret := func(s string) string {
			if s != "" {
				return "********"
			}
			return ""
		}
		return c.JSON(http.StatusOK, map[string]interface{}{
			"metrics_retention_days": settings.MetricsRetentionDays,
			"smtp_host":              settings.SmtpHost,
			"smtp_port":              settings.SmtpPort,
			"smtp_user":              settings.SmtpUser,
			"smtp_pass":              maskSecret(settings.SmtpPass),
			"google_client_id":       settings.GoogleClientID,
			"google_client_secret":   maskSecret(settings.GoogleClientSecret),
			"slack_webhook_url":      settings.SlackWebhookUrl,
			"msteams_webhook_url":    settings.MSTeamsWebhookUrl,
			"gchat_webhook_url":      settings.GChatWebhookUrl,
			"generic_webhook_url":    settings.GenericWebhookUrl,
			"alerts_email_address":   settings.AlertsEmailAddress,
			"backup_enabled":         settings.BackupEnabled,
			"backup_provider":        settings.BackupProvider,
			"backup_cron":            settings.BackupCron,
			"backup_bucket":          settings.BackupBucket,
			"backup_region":          settings.BackupRegion,
			"backup_endpoint":        settings.BackupEndpoint,
			"backup_auth1":           settings.BackupAuth1,
			"backup_auth2":           maskSecret(settings.BackupAuth2),
			"archival_enabled":       settings.ArchivalEnabled,
			"archive_metrics":        settings.ArchiveMetrics,
			"archive_logs":           settings.ArchiveLogs,
			"archival_provider":      settings.ArchivalProvider,
			"archival_cron":          settings.ArchivalCron,
			"archival_bucket":        settings.ArchivalBucket,
			"archival_region":        settings.ArchivalRegion,
			"archival_endpoint":      settings.ArchivalEndpoint,
			"archival_auth1":         settings.ArchivalAuth1,
			"archival_auth2":         maskSecret(settings.ArchivalAuth2),
		})
	})

	admin.PUT("/settings", func(c echo.Context) error {
		var payload struct {
			MetricsRetentionDays int    `json:"metrics_retention_days"`
			SmtpHost             string `json:"smtp_host"`
			SmtpPort             int    `json:"smtp_port"`
			SmtpUser             string `json:"smtp_user"`
			SmtpPass             string `json:"smtp_pass"`
			GoogleClientID       string `json:"google_client_id"`
			GoogleClientSecret   string `json:"google_client_secret"`
			SlackWebhookUrl      string `json:"slack_webhook_url"`
			MSTeamsWebhookUrl    string `json:"msteams_webhook_url"`
			GChatWebhookUrl      string `json:"gchat_webhook_url"`
			GenericWebhookUrl    string `json:"generic_webhook_url"`
			AlertsEmailAddress   string `json:"alerts_email_address"`
			BackupEnabled        bool   `json:"backup_enabled"`
			BackupProvider       string `json:"backup_provider"`
			BackupCron           string `json:"backup_cron"`
			BackupBucket         string `json:"backup_bucket"`
			BackupRegion         string `json:"backup_region"`
			BackupEndpoint       string `json:"backup_endpoint"`
			BackupAuth1          string `json:"backup_auth1"`
			BackupAuth2          string `json:"backup_auth2"`
			ArchivalEnabled      bool   `json:"archival_enabled"`
			ArchiveMetrics       bool   `json:"archive_metrics"`
			ArchiveLogs          bool   `json:"archive_logs"`
			ArchivalProvider     string `json:"archival_provider"`
			ArchivalCron         string `json:"archival_cron"`
			ArchivalBucket       string `json:"archival_bucket"`
			ArchivalRegion       string `json:"archival_region"`
			ArchivalEndpoint     string `json:"archival_endpoint"`
			ArchivalAuth1        string `json:"archival_auth1"`
			ArchivalAuth2        string `json:"archival_auth2"`
		}
		if err := c.Bind(&payload); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request"})
		}

		var s db.Setting
		db.GormDB.First(&s, 1)

		if payload.SmtpPass == "********" {
			payload.SmtpPass = s.SmtpPass
		}
		if payload.GoogleClientSecret == "********" {
			payload.GoogleClientSecret = s.GoogleClientSecret
		}
		if payload.BackupAuth2 == "********" {
			payload.BackupAuth2 = s.BackupAuth2
		}
		if payload.ArchivalAuth2 == "********" {
			payload.ArchivalAuth2 = s.ArchivalAuth2
		}

		err := db.GormDB.Model(&db.Setting{}).Where("id = ?", 1).Updates(map[string]interface{}{
			"metrics_retention_days": payload.MetricsRetentionDays,
			"smtp_host":              payload.SmtpHost,
			"smtp_port":              payload.SmtpPort,
			"smtp_user":              payload.SmtpUser,
			"smtp_pass":              payload.SmtpPass,
			"google_client_id":       payload.GoogleClientID,
			"google_client_secret":   payload.GoogleClientSecret,
			"slack_webhook_url":      payload.SlackWebhookUrl,
			"ms_teams_webhook_url":   payload.MSTeamsWebhookUrl,
			"g_chat_webhook_url":     payload.GChatWebhookUrl,
			"generic_webhook_url":    payload.GenericWebhookUrl,
			"alerts_email_address":   payload.AlertsEmailAddress,
			"backup_enabled":         payload.BackupEnabled,
			"backup_provider":        payload.BackupProvider,
			"backup_cron":            payload.BackupCron,
			"backup_bucket":          payload.BackupBucket,
			"backup_region":          payload.BackupRegion,
			"backup_endpoint":        payload.BackupEndpoint,
			"backup_auth1":           payload.BackupAuth1,
			"backup_auth2":           payload.BackupAuth2,
			"archival_enabled":       payload.ArchivalEnabled,
			"archive_metrics":        payload.ArchiveMetrics,
			"archive_logs":           payload.ArchiveLogs,
			"archival_provider":      payload.ArchivalProvider,
			"archival_cron":          payload.ArchivalCron,
			"archival_bucket":        payload.ArchivalBucket,
			"archival_region":        payload.ArchivalRegion,
			"archival_endpoint":      payload.ArchivalEndpoint,
			"archival_auth1":         payload.ArchivalAuth1,
			"archival_auth2":         payload.ArchivalAuth2,
		}).Error

		if err != nil {
			log.Println("Failed to update global settings:", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to update settings"})
		}

		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		logAudit(userClaims.ID, userClaims.Username, "UPDATE_SETTINGS", "GlobalSettings", "Success", "Updated global settings including SMTP, OAuth, and Backups")

		// Reload backup and archival cron schedulers
		backup.ReloadSchedule()
		archival.ReloadSchedule()

		return c.NoContent(http.StatusOK)
	})

	admin.POST("/settings/backup/test", func(c echo.Context) error {
		var s db.Setting
		db.GormDB.First(&s, 1)

		if err := backup.RunBackup(s); err != nil {
			log.Printf("Test backup failed: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Backup failed. Check server logs for details."})
		}
		return c.JSON(http.StatusOK, map[string]string{"message": "Backup successful"})
	})

	admin.POST("/settings/archival/test", func(c echo.Context) error {
		var s db.Setting
		db.GormDB.First(&s, 1)

		if err := archival.RunArchival(s); err != nil {
			log.Printf("Test archival failed: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Archival failed. Check server logs for details."})
		}
		return c.JSON(http.StatusOK, map[string]string{"message": "Archival successful"})
	})

	admin.GET("/alerts/rules/:id", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		var r alerts.AlertRule
		if err := db.GormDB.First(&r, id).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Rule not found"})
		}
		return c.JSON(http.StatusOK, r)
	})

	// CREATE rule
	admin.POST("/alerts/rules", func(c echo.Context) error {
		name := strings.TrimSpace(c.FormValue("name"))
		if name == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
		}
		containerPattern := c.FormValue("container_pattern")
		if containerPattern == "" {
			containerPattern = ".*"
		}
		if _, err := regexp.Compile(containerPattern); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid container_pattern regex: " + err.Error()})
		}
		logPattern := c.FormValue("log_pattern")
		if logPattern != "" {
			if _, err := regexp.Compile(logPattern); err != nil {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid log_pattern regex: " + err.Error()})
			}
		}
		eventTypes := c.FormValue("event_types")
		enabled := c.FormValue("enabled") != "false"
		cooldown := 300
		if v, err := strconv.Atoi(c.FormValue("cooldown_seconds")); err == nil && v > 0 {
			cooldown = v
		}

		enableSlack := c.FormValue("enable_slack") == "true"
		enableMSTeams := c.FormValue("enable_msteams") == "true"
		enableGChat := c.FormValue("enable_gchat") == "true"
		enableGenericWebhook := c.FormValue("enable_generic_webhook") == "true"
		enableEmail := c.FormValue("enable_email") == "true"
		emailAddress := strings.TrimSpace(c.FormValue("email_address"))
		metricCPUThreshold, _ := strconv.ParseFloat(c.FormValue("metric_cpu_threshold"), 64)
		metricMemThreshold, _ := strconv.ParseFloat(c.FormValue("metric_mem_threshold"), 64)

		r := alerts.AlertRule{
			Name: name, ContainerPattern: containerPattern, EventTypes: eventTypes, LogPattern: logPattern,
			Enabled: enabled, CooldownSeconds: cooldown,
			EnableSlack: enableSlack, EnableMSTeams: enableMSTeams, EnableGChat: enableGChat,
			EnableGenericWebhook: enableGenericWebhook, EnableEmail: enableEmail, EmailAddress: emailAddress,
			MetricCPUThreshold: metricCPUThreshold, MetricMemThreshold: int64(metricMemThreshold),
		}

		db.GormDB.Create(&r)

		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		logAudit(userClaims.ID, userClaims.Username, "CREATE_ALERT_RULE", "Rule:"+name, "Success", "Created alert rule: "+name)

		alertMgr.ReloadRules()
		return c.JSON(http.StatusCreated, map[string]interface{}{"id": r.ID})
	})

	// GitOps API
	r.GET("/gitops/projects", func(c echo.Context) error {
		var projects []db.GitProject
		if err := db.GormDB.Find(&projects).Error; err != nil {
			log.Printf("Failed to fetch GitOps projects: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch projects"})
		}

		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)

		if !userClaims.IsAdmin {
			patterns := getAuthorizedPatterns(userClaims.ID)
			var filtered []db.GitProject
			for _, p := range projects {
				authorized := false
				for _, pat := range patterns {
					if matched, _ := regexp.MatchString(pat, p.Name); matched {
						authorized = true
						break
					}
				}
				if authorized {
					filtered = append(filtered, p)
				}
			}
			projects = filtered
		}

		return c.JSON(http.StatusOK, projects)
	})

	r.POST("/gitops/projects", func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanCreateDeployments {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Access Denied"})
		}

		var project db.GitProject
		if err := c.Bind(&project); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid payload"})
		}

		if strings.HasPrefix(project.Branch, "-") || strings.HasPrefix(project.RepoURL, "-") {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid branch or repo URL"})
		}
		if strings.Contains(project.ComposePath, "..") || strings.HasPrefix(project.ComposePath, "/") {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid Compose path"})
		}

		project.Status = "pending"
		if err := db.GormDB.Create(&project).Error; err != nil {
			log.Printf("Failed to create GitOps project: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create project"})
		}
		return c.JSON(http.StatusOK, project)
	})

	r.PUT("/gitops/projects/:id", func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanEditDeployments {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Access Denied"})
		}

		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		var project db.GitProject
		if err := db.GormDB.First(&project, id).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Project not found"})
		}

		if !userClaims.IsAdmin {
			patterns := getAuthorizedPatterns(userClaims.ID)
			authorized := false
			for _, p := range patterns {
				if matched, _ := regexp.MatchString(p, project.Name); matched {
					authorized = true
					break
				}
			}
			if !authorized {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "Security Restriction: You are not authorized to interact with this project resource."})
			}
		}

		var updateData struct {
			ComposeContent string `json:"compose_content"`
			Branch         string `json:"branch"`
			ComposePath    string `json:"compose_path"`
		}
		if err := c.Bind(&updateData); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid payload"})
		}

		if strings.HasPrefix(updateData.Branch, "-") {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid branch"})
		}
		if strings.Contains(updateData.ComposePath, "..") || strings.HasPrefix(updateData.ComposePath, "/") {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid Compose path"})
		}

		updates := map[string]interface{}{
			"status": "pending", // Trigger a redeploy
		}
		if project.SourceType == "inline" {
			updates["compose_content"] = updateData.ComposeContent
		} else {
			updates["branch"] = updateData.Branch
			updates["compose_path"] = updateData.ComposePath
		}

		if err := db.GormDB.Model(&project).Updates(updates).Error; err != nil {
			log.Printf("Failed to update GitOps project: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to update project"})
		}
		return c.JSON(http.StatusOK, project)
	})

	r.POST("/gitops/projects/:id/sync", func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanEditDeployments {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Access Denied"})
		}

		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		var project db.GitProject
		if err := db.GormDB.First(&project, id).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Project not found"})
		}

		if !userClaims.IsAdmin {
			patterns := getAuthorizedPatterns(userClaims.ID)
			authorized := false
			for _, p := range patterns {
				if matched, _ := regexp.MatchString(p, project.Name); matched {
					authorized = true
					break
				}
			}
			if !authorized {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "Security Restriction: You are not authorized to interact with this project resource."})
			}
		}

		if err := db.GormDB.Model(&project).Update("status", "pending").Error; err != nil {
			log.Printf("Failed to trigger sync: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to trigger sync"})
		}
		return c.JSON(http.StatusOK, map[string]string{"message": "Sync triggered"})
	})

	r.DELETE("/gitops/projects/:id", func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanDeleteDeployments {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Access Denied"})
		}

		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		var project db.GitProject
		if err := db.GormDB.First(&project, id).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Project not found"})
		}

		if !userClaims.IsAdmin {
			patterns := getAuthorizedPatterns(userClaims.ID)
			authorized := false
			for _, p := range patterns {
				if matched, _ := regexp.MatchString(p, project.Name); matched {
					authorized = true
					break
				}
			}
			if !authorized {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "Security Restriction: You are not authorized to interact with this project resource."})
			}
		}

		if err := db.GormDB.Delete(&db.GitProject{}, id).Error; err != nil {
			log.Printf("Failed to delete GitOps project: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to delete project"})
		}
		return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
	})

	r.GET("/gitops/projects/:id/deployments", func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)

		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}

		var project db.GitProject
		if err := db.GormDB.First(&project, id).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Project not found"})
		}

		if !userClaims.IsAdmin {
			patterns := getAuthorizedPatterns(userClaims.ID)
			authorized := false
			for _, p := range patterns {
				if matched, _ := regexp.MatchString(p, project.Name); matched {
					authorized = true
					break
				}
			}
			if !authorized {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "Security Restriction: You are not authorized to interact with this project resource."})
			}
		}

		var deployments []db.GitDeployment
		if err := db.GormDB.Where("project_id = ?", id).Order("created_at desc").Find(&deployments).Error; err != nil {
			log.Printf("Failed to fetch deployments: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch deployments"})
		}
		return c.JSON(http.StatusOK, deployments)
	})

	// UPDATE rule
	admin.PUT("/alerts/rules/:id", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}

		var r alerts.AlertRule
		if err := db.GormDB.First(&r, id).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Rule not found"})
		}

		name := strings.TrimSpace(c.FormValue("name"))
		if name == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
		}

		containerPattern := c.FormValue("container_pattern")
		if containerPattern == "" {
			containerPattern = ".*"
		}
		if _, err := regexp.Compile(containerPattern); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid container_pattern regex: " + err.Error()})
		}

		logPattern := c.FormValue("log_pattern")
		if logPattern != "" {
			if _, err := regexp.Compile(logPattern); err != nil {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid log_pattern regex: " + err.Error()})
			}
		}

		eventTypes := c.FormValue("event_types")
		enabled := c.FormValue("enabled") != "false"
		cooldown := 300
		if v, err := strconv.Atoi(c.FormValue("cooldown_seconds")); err == nil && v > 0 {
			cooldown = v
		}

		enableSlack := c.FormValue("enable_slack") == "true"
		enableMSTeams := c.FormValue("enable_msteams") == "true"
		enableGChat := c.FormValue("enable_gchat") == "true"
		enableGenericWebhook := c.FormValue("enable_generic_webhook") == "true"
		enableEmail := c.FormValue("enable_email") == "true"
		emailAddress := strings.TrimSpace(c.FormValue("email_address"))
		metricCPUThreshold, _ := strconv.ParseFloat(c.FormValue("metric_cpu_threshold"), 64)
		metricMemThreshold, _ := strconv.ParseFloat(c.FormValue("metric_mem_threshold"), 64)

		db.GormDB.Model(&r).Updates(map[string]interface{}{
			"name":                   name,
			"container_pattern":      containerPattern,
			"event_types":            eventTypes,
			"log_pattern":            logPattern,
			"enabled":                enabled,
			"cooldown_seconds":       cooldown,
			"enable_slack":           enableSlack,
			"enable_msteams":         enableMSTeams,
			"enable_gchat":           enableGChat,
			"enable_generic_webhook": enableGenericWebhook,
			"enable_email":           enableEmail,
			"email_address":          emailAddress,
			"metric_cpu_threshold":   metricCPUThreshold,
			"metric_mem_threshold":   metricMemThreshold,
		})

		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		logAudit(userClaims.ID, userClaims.Username, "UPDATE_ALERT_RULE", "Rule:"+name, "Success", "Updated alert rule: "+name)

		alertMgr.ReloadRules()
		return c.NoContent(http.StatusOK)
	})

	// DELETE rule
	admin.DELETE("/alerts/rules/:id", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		db.GormDB.Delete(&alerts.AlertRule{}, id)

		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		logAudit(userClaims.ID, userClaims.Username, "DELETE_ALERT_RULE", "Rule:"+id, "Success", "Deleted alert rule ID: "+id)

		alertMgr.ReloadRules()
		return c.NoContent(http.StatusOK)
	})

	// TOGGLE enabled/disabled without full PUT
	admin.PUT("/alerts/rules/:id/toggle", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}
		enabled := c.FormValue("enabled") != "false"
		db.GormDB.Model(&alerts.AlertRule{}).Where("id = ?", id).Update("enabled", enabled)
		alertMgr.ReloadRules()
		return c.NoContent(http.StatusOK)
	})

	// LIST alert history
	// DELETE all alert history
	admin.DELETE("/alerts/history", func(c echo.Context) error {
		if err := db.GormDB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&db.AlertHistory{}).Error; err != nil {
			log.Printf("[API] Failed to clear alert history: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to clear history"})
		}
		return c.NoContent(http.StatusOK)
	})
	admin.GET("/alerts/history", func(c echo.Context) error {
		limitStr := c.QueryParam("limit")
		limit := 100
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 500 {
			limit = v
		}
		ruleID := c.QueryParam("rule_id")

		query := db.GormDB.Model(&db.AlertHistory{})
		if ruleID != "" {
			query = query.Where("rule_id = ?", ruleID)
		}

		var history []db.AlertHistory
		query.Order("timestamp DESC").Limit(limit).Find(&history)
		return c.JSON(http.StatusOK, history)
	})

	e.GET("/ws/system-stats", func(c echo.Context) error {
		_, err := authenticateWS(c)
		if err != nil {
			return wsAuthError(c, err)
		}

		ws, err := upgradeAuthenticatedWS(c)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return nil
		}
		defer ws.Close()

		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				sysStatsMu.RLock()
				data := latestSystemStats
				sysStatsMu.RUnlock()

				if data != nil {
					if err := ws.WriteJSON(data); err != nil {
						return nil
					}
				}
			case <-c.Request().Context().Done():
				return nil
			}
		}
	})

	e.GET("/ws/events", func(c echo.Context) error {
		userClaims, err := authenticateWS(c)
		if err != nil {
			return wsAuthError(c, err)
		}

		ws, err := upgradeAuthenticatedWS(c)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return nil
		}
		defer ws.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		eventsRes := cli.Events(ctx, client.EventsListOptions{})
		messages, errs := eventsRes.Messages, eventsRes.Err

		for {
			select {
			case msg := <-messages:
				// Filter container events or pass all events based on permissions
				if !userClaims.IsAdmin {
					containerName := msg.Actor.Attributes["name"]
					if containerName == "" {
						continue // If name is empty, skip for restricted users
					}
					patterns := getAuthorizedPatterns(userClaims.ID)
					authorized := false
					for _, p := range patterns {
						if matched, _ := regexp.MatchString(p, containerName); matched {
							authorized = true
							break
						}
					}
					if !authorized {
						continue
					}
				}

				if err := ws.WriteJSON(msg); err != nil {
					return nil
				}
			case <-errs:
				return nil
			case <-c.Request().Context().Done():
				return nil
			}
		}
	})

	e.GET("/ws/logs/:id", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}

		userClaims, err := authenticateWS(c)
		if err != nil {
			return wsAuthError(c, err)
		}

		container, err := cli.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{})
		if err != nil {
			return c.NoContent(http.StatusNotFound)
		}
		wsLogsImage := ""
		if container.Container.Config != nil {
			wsLogsImage = container.Container.Config.Image
		}
		if inspectContainerExcluded(userClaims.IsAdmin, container.Container.Name, wsLogsImage) {
			return c.NoContent(http.StatusNotFound)
		}
		containerName := strings.TrimPrefix(container.Container.Name, "/")

		if !userClaims.IsAdmin {
			patterns := getAuthorizedPatterns(userClaims.ID)
			authorized := false
			for _, p := range patterns {
				if matched, _ := regexp.MatchString(p, containerName); matched {
					authorized = true
					break
				}
			}
			if !authorized {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "Access Denied: You do not have permission to view logs for this resource."})
			}
		}

		ws, err := upgradeAuthenticatedWS(c)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return nil
		}
		defer ws.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		out, err := cli.ContainerLogs(ctx, id, client.ContainerLogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     true,
			Tail:       "100",
			Timestamps: true,
		})
		if err != nil {
			log.Printf("Failed to fetch container logs for WS: %v", err)
			ws.WriteMessage(websocket.TextMessage, []byte("\r\n[LightHouse] Failed to fetch logs\r\n"))
			return nil
		}
		defer out.Close()

		header := make([]byte, 8)
		for {
			_, err = io.ReadFull(out, header)
			if err != nil {
				break
			}

			size := uint32(header[4])<<24 | uint32(header[5])<<16 | uint32(header[6])<<8 | uint32(header[7])
			payload := make([]byte, size)
			_, err = io.ReadFull(out, payload)
			if err != nil {
				break
			}

			if err := ws.WriteMessage(websocket.TextMessage, payload); err != nil {
				break
			}
		}
		return nil
	})

	e.GET("/ws/shell/:id", func(c echo.Context) error {
		id := c.Param("id")
		if !isValidContainerID(id) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container ID format"})
		}

		userClaims, err := authenticateWS(c)
		if err != nil {
			return wsAuthError(c, err)
		}

		if !AllowShell {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Shell access is disabled on this server."})
		}

		var canShell bool
		err = db.DB.QueryRow("SELECT can_shell FROM users WHERE id = ? AND is_active = 1", userClaims.ID).Scan(&canShell)
		if err != nil || !canShell {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Shell access is not permitted for this account."})
		}

		// Verify container exists and get its name
		container, err := cli.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{})
		if err != nil {
			return c.NoContent(http.StatusNotFound)
		}
		shellImage := ""
		if container.Container.Config != nil {
			shellImage = container.Container.Config.Image
		}
		if inspectContainerExcluded(userClaims.IsAdmin, container.Container.Name, shellImage) {
			return c.NoContent(http.StatusNotFound)
		}
		containerName := strings.TrimPrefix(container.Container.Name, "/")

		// Regex Access Check
		if !userClaims.IsAdmin {
			patterns := getAuthorizedPatterns(userClaims.ID)
			authorized := false
			for _, p := range patterns {
				if matched, _ := regexp.MatchString(p, containerName); matched {
					authorized = true
					break
				}
			}
			if !authorized {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "Access Denied: You do not have permission to view this resource."})
			}
		}

		shellCmd := c.QueryParam("shell")
		if shellCmd == "" {
			shellCmd = "/bin/sh"
		}
		allowedShells := map[string]bool{"/bin/sh": true, "/bin/bash": true, "/bin/ash": true}
		if !allowedShells[shellCmd] {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid shell"})
		}

		ws, err := upgradeAuthenticatedWS(c)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return nil
		}
		defer ws.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		execConfig := client.ExecCreateOptions{
			AttachStdin:  true,
			AttachStdout: true,
			AttachStderr: true,
			TTY:          true,
			Cmd:          []string{shellCmd},
		}

		execResult, err := cli.ExecCreate(ctx, id, execConfig)
		if err != nil {
			log.Printf("Failed to create exec session for container %s: %v", id, err)
			ws.WriteMessage(websocket.TextMessage, []byte("\r\n[LightHouse] Failed to create terminal session. The container may not support this shell.\r\n"))
			return nil
		}

		// Attach exec
		attachResult, err := cli.ExecAttach(ctx, execResult.ID, client.ExecAttachOptions{
			TTY: true,
		})
		if err != nil {
			log.Printf("Failed to attach exec session for container %s: %v", id, err)
			ws.WriteMessage(websocket.TextMessage, []byte("\r\n[LightHouse] Failed to attach to terminal session. Please try again.\r\n"))
			return nil
		}
		defer attachResult.Close()

		errChan := make(chan error, 2)
		go func() {
			for {
				_, msg, err := ws.ReadMessage()
				if err != nil {
					errChan <- err
					return
				}

				_, err = attachResult.Conn.Write(msg)
				if err != nil {
					errChan <- err
					return
				}
			}
		}()

		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := attachResult.Reader.Read(buf)
				if n > 0 {
					err = ws.WriteMessage(websocket.TextMessage, buf[:n])
					if err != nil {
						errChan <- err
						return
					}
				}
				if err != nil {
					errChan <- err
					return
				}
			}
		}()

		<-errChan
		return nil
	})

	// Serve Frontend (skipped in agent-only mode)
	if serveFrontend {
		e.Use(middleware.StaticWithConfig(middleware.StaticConfig{
			Root:   "frontend/dist",
			Browse: false,
			HTML5:  true,
			Skipper: func(c echo.Context) bool {
				return strings.HasPrefix(c.Path(), "/api") || strings.HasPrefix(c.Path(), "/ws")
			},
		}))
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}

	log.Printf("LightHouse %s listening on %s\n", Version, port)
	e.Logger.Fatal(e.Start(port))
}

func extractContainers(res interface{}) []map[string]interface{} {
	b, _ := json.Marshal(res)
	var m interface{}
	json.Unmarshal(b, &m)

	if list, ok := m.([]interface{}); ok {
		var ret []map[string]interface{}
		for _, item := range list {
			if mm, ok := item.(map[string]interface{}); ok {
				ret = append(ret, mm)
			}
		}
		return ret
	}
	if mm, ok := m.(map[string]interface{}); ok {
		for _, val := range mm {
			if list, ok := val.([]interface{}); ok {
				var ret []map[string]interface{}
				for _, item := range list {
					if mmm, ok := item.(map[string]interface{}); ok {
						ret = append(ret, mmm)
					}
				}
				return ret
			}
		}
	}
	return nil
}

var (
	latestSystemStats map[string]interface{}
	sysStatsMu        sync.RWMutex
)

func systemStatsBroadcaster(cli *client.Client) {
	for {
		v, _ := mem.VirtualMemory()
		cp, _ := cpu.Percent(500*time.Millisecond, false)
		cpuVal := 0.0
		if len(cp) > 0 {
			cpuVal = cp[0]
		}

		cores, err := cpu.Counts(true)
		if err != nil || cores == 0 {
			cores = runtime.NumCPU()
		}

		runningContainers := 0
		totalContainers := 0
		if cli != nil {
			res, err := cli.ContainerList(context.Background(), client.ContainerListOptions{All: true})
			if err == nil {
				containers := extractContainers(res.Items)
				totalContainers = len(containers)
				for _, c := range containers {
					if c["state"] == "running" {
						runningContainers++
					}
				}
			}
		}

		sysStatsMu.Lock()
		latestSystemStats = map[string]interface{}{
			"cpu":                cpuVal,
			"memory":             v.Used,
			"total_memory":       v.Total,
			"cores":              cores,
			"running_containers": runningContainers,
			"total_containers":   totalContainers,
		}
		sysStatsMu.Unlock()

		time.Sleep(500 * time.Millisecond) // Total cycle ~1s (500ms sample + 500ms sleep)
	}
}

func getRetentionDays() int {
	var days int
	err := db.DB.QueryRow("SELECT metrics_retention_days FROM settings WHERE id = 1").Scan(&days)
	if err != nil || days <= 0 {
		return 30
	}
	return days
}

func startStatsCollector(cli *client.Client) {
	go systemStatsBroadcaster(cli)
	// Initial collection
	collectStats(cli)

	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for range ticker.C {
			collectStats(cli)
			retentionDays := getRetentionDays()
			db.DB.Exec("DELETE FROM stats WHERE timestamp < datetime('now', '-' || ? || ' days')", retentionDays)
			db.DB.Exec("DELETE FROM system_stats WHERE timestamp < datetime('now', '-' || ? || ' days')", retentionDays)
		}
	}()
}

var (
	prevStats = make(map[string]struct {
		TotalUsage  uint64
		SystemUsage uint64
		NetRx       uint64
		NetTx       uint64
		DiskRead    uint64
		DiskWrite   uint64
	})
	prevStatsMu sync.Mutex
)

func collectStats(cli *client.Client) {
	// System Stats
	v, _ := mem.VirtualMemory()
	cp, _ := cpu.Percent(time.Second, false)
	netStats, _ := net.IOCounters(false)
	diskStats, _ := disk.IOCounters()

	var hostNetRx, hostNetTx, hostDiskRead, hostDiskWrite uint64
	if len(netStats) > 0 {
		hostNetRx = netStats[0].BytesRecv
		hostNetTx = netStats[0].BytesSent
	}
	for _, stat := range diskStats {
		hostDiskRead += stat.ReadBytes
		hostDiskWrite += stat.WriteBytes
	}

	var cpVal float64
	if len(cp) > 0 {
		cpVal = cp[0]
	}

	var sysRxDelta, sysTxDelta, sysReadDelta, sysWriteDelta uint64
	prevStatsMu.Lock()
	prevHost, ok := prevStats["__HOST__"]
	if ok {
		if hostNetRx > prevHost.NetRx {
			sysRxDelta = hostNetRx - prevHost.NetRx
		}
		if hostNetTx > prevHost.NetTx {
			sysTxDelta = hostNetTx - prevHost.NetTx
		}
		if hostDiskRead > prevHost.DiskRead {
			sysReadDelta = hostDiskRead - prevHost.DiskRead
		}
		if hostDiskWrite > prevHost.DiskWrite {
			sysWriteDelta = hostDiskWrite - prevHost.DiskWrite
		}
	}
	prevStats["__HOST__"] = struct {
		TotalUsage  uint64
		SystemUsage uint64
		NetRx       uint64
		NetTx       uint64
		DiskRead    uint64
		DiskWrite   uint64
	}{
		NetRx:     hostNetRx,
		NetTx:     hostNetTx,
		DiskRead:  hostDiskRead,
		DiskWrite: hostDiskWrite,
	}
	prevStatsMu.Unlock()

	sysStat := db.SystemStat{
		CPU:            cpVal,
		Memory:         int64(v.Used),
		NetRxBytes:     int64(sysRxDelta),
		NetTxBytes:     int64(sysTxDelta),
		DiskReadBytes:  int64(sysReadDelta),
		DiskWriteBytes: int64(sysWriteDelta),
	}
	if LighthouseMode == "spoke" {
		cluster.PushToHub("system_stat", sysStat)
	} else {
		db.GormDB.Create(&sysStat)
	}

	// Container Stats Snapshot
	res, _ := cli.ContainerList(context.Background(), client.ContainerListOptions{})
	containers := extractContainers(res.Items)
	for _, ctr := range containers {
		id, _ := ctr["ID"].(string)
		if id == "" {
			id, _ = ctr["Id"].(string)
		}
		state, _ := ctr["State"].(string)
		if state != "running" {
			continue
		}
		stats, err := cli.ContainerStats(context.Background(), id, client.ContainerStatsOptions{Stream: false})
		if err != nil {
			continue
		}
		var s struct {
			CPUStats struct {
				CPUUsage struct {
					TotalUsage uint64 `json:"total_usage"`
				} `json:"cpu_usage"`
				SystemUsage uint64 `json:"system_cpu_usage"`
				OnlineCPUs  uint32 `json:"online_cpus"`
			} `json:"cpu_stats"`
			MemoryStats struct {
				Usage uint64            `json:"usage"`
				Stats map[string]uint64 `json:"stats"`
			} `json:"memory_stats"`
			Networks map[string]struct {
				RxBytes uint64 `json:"rx_bytes"`
				TxBytes uint64 `json:"tx_bytes"`
			} `json:"networks"`
			BlkioStats struct {
				IoServiceBytesRecursive []struct {
					Op    string `json:"op"`
					Value uint64 `json:"value"`
				} `json:"io_service_bytes_recursive"`
			} `json:"blkio_stats"`
		}
		if err := json.NewDecoder(stats.Body).Decode(&s); err != nil {
			stats.Body.Close()
			continue
		}
		stats.Body.Close()

		var curRx, curTx, curRead, curWrite uint64
		for _, netIf := range s.Networks {
			curRx += netIf.RxBytes
			curTx += netIf.TxBytes
		}
		for _, ioStat := range s.BlkioStats.IoServiceBytesRecursive {
			switch op := strings.ToLower(ioStat.Op); op {
			case "read":
				curRead += ioStat.Value
			case "write":
				curWrite += ioStat.Value
			}
		}

		cpuPercent := 0.0
		var rxDelta, txDelta, readDelta, writeDelta uint64

		prevStatsMu.Lock()
		prev, ok := prevStats[id]
		if ok {
			cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(prev.TotalUsage)
			systemDelta := float64(s.CPUStats.SystemUsage) - float64(prev.SystemUsage)

			onlineCPUs := float64(s.CPUStats.OnlineCPUs)
			if onlineCPUs == 0 {
				onlineCPUs = float64(runtime.NumCPU())
			}

			if systemDelta > 0 && cpuDelta > 0 {
				cpuPercent = (cpuDelta / systemDelta) * onlineCPUs * 100.0
			}

			if curRx > prev.NetRx {
				rxDelta = curRx - prev.NetRx
			}
			if curTx > prev.NetTx {
				txDelta = curTx - prev.NetTx
			}
			if curRead > prev.DiskRead {
				readDelta = curRead - prev.DiskRead
			}
			if curWrite > prev.DiskWrite {
				writeDelta = curWrite - prev.DiskWrite
			}
		}
		prevStats[id] = struct {
			TotalUsage  uint64
			SystemUsage uint64
			NetRx       uint64
			NetTx       uint64
			DiskRead    uint64
			DiskWrite   uint64
		}{
			TotalUsage:  s.CPUStats.CPUUsage.TotalUsage,
			SystemUsage: s.CPUStats.SystemUsage,
			NetRx:       curRx,
			NetTx:       curTx,
			DiskRead:    curRead,
			DiskWrite:   curWrite,
		}
		prevStatsMu.Unlock()

		// cgroups v2 uses "inactive_file", cgroups v1 uses "cache".
		// Docker recommends subtracting inactive_file for accurate working-set memory.
		memUsed := s.MemoryStats.Usage
		if inactiveFile, ok := s.MemoryStats.Stats["inactive_file"]; ok && inactiveFile < memUsed {
			memUsed -= inactiveFile
		} else if cache, ok := s.MemoryStats.Stats["cache"]; ok && cache < memUsed {
			memUsed -= cache
		}
		stat := db.Stat{
			ContainerID:    id,
			CPU:            cpuPercent,
			Memory:         int64(memUsed),
			NetRxBytes:     int64(rxDelta),
			NetTxBytes:     int64(txDelta),
			DiskReadBytes:  int64(readDelta),
			DiskWriteBytes: int64(writeDelta),
		}
		if LighthouseMode == "spoke" {
			cluster.PushToHub("stat", stat)
		} else {
			db.GormDB.Create(&stat)
		}
	}
}

func seedAdmin() {
	var count int64
	db.GormDB.Model(&db.User{}).Where("username = ?", "admin").Count(&count)
	if count == 0 {
		const plain = "admin123"
		log.Println("Default admin account created (username: admin, password: admin123). Change the password on first login.")

		h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
		if err != nil {
			log.Fatalf("Failed to hash default admin password: %v", err)
		}

		adminUser := db.User{
			Username:        "admin",
			Password:        string(h),
			IsAdmin:         true,
			CanStart:        true,
			CanStop:         true,
			CanRestart:      true,
			CanDelete:       true,
			CanShell:        true,
			PasswordChanged: false,
		}
		if err := db.GormDB.Create(&adminUser).Error; err != nil {
			log.Fatalf("Failed to create default admin: %v", err)
		}
	}

	db.GormDB.Model(&db.User{}).Where("is_admin = ?", true).Updates(map[string]interface{}{
		"can_start": true, "can_stop": true, "can_restart": true, "can_delete": true, "can_shell": true,
	})
}


func isValidContainerID(id string) bool {
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, id)
	return matched
}

func cleanupStaleAlerts() {
	res := db.GormDB.Model(&db.AlertHistory{}).
		Where("delivery_status = ?", "").
		Update("delivery_status", "Failed (Stale)")
	if res.Error != nil {
		log.Printf("Failed to cleanup stale alerts: %v", res.Error)
	} else if res.RowsAffected > 0 {
		log.Printf("Cleaned up %d stale pending alerts.", res.RowsAffected)
	}
}
