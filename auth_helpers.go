// This file implements the cross-cutting security plumbing shared by every
// handler in package main: JWT secret bootstrapping, CORS/Origin/Referer
// validation, WebSocket subprotocol authentication, login rate limiting,
// security response headers, and the RBAC permission-clamping helpers.
package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"lighthouse/db"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

// defaultSecretKey is the fallback JWT signing key used only outside
// production; initSecretKey refuses to start in production with this value.
const defaultSecretKey = "secret-key-change-this"

// loginRateLimiter tracks recent failed-login timestamps per key (typically
// client IP) to enforce brute-force protection on /api/token.
type loginRateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

// loginRateLimit is the process-wide login rate limiter instance.
var loginRateLimit loginRateLimiter

var (
	ClientAccessEnabled bool
	allowedOrigins      []string
	TrustProxy          bool
)

const (
	clientHeaderWeb        = "web"
	headerLightHouseClient = "X-LightHouse-Client"
	minPasswordLength      = 8
	maxContainerPatternLen = 256
)

// initSecretKey loads SECRET_KEY from the environment into the package-level
// SECRET_KEY used to sign/verify all JWTs. It refuses to boot in production
// with the insecure default, and warns loudly everywhere else.
func initSecretKey() {
	key := os.Getenv("SECRET_KEY")
	if key == "" {
		key = defaultSecretKey
	}
	SECRET_KEY = []byte(key)

	if key == defaultSecretKey {
		env := os.Getenv("ENV")
		if env == "production" || os.Getenv("GO_ENV") == "production" {
			log.Fatalf("SECRET_KEY must be set in production")
		}
		log.Println("WARNING: Using default SECRET_KEY. Set the SECRET_KEY environment variable before deploying.")
	}
}

// initWSUpgrader wires the shared websocket.Upgrader's CheckOrigin callback
// to isWSAccessAllowed, enforcing the same Origin policy on every WS endpoint.
func initWSUpgrader() {
	upgrader.CheckOrigin = isWSAccessAllowed
}

// initClientAccess reads the CLIENT_ACCESS/ALLOWED_ORIGINS/TRUST_PROXY
// environment variables at boot to configure the client-origin enforcement
// used by clientAccessMiddleware and CORS.
func initClientAccess() {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("CLIENT_ACCESS")))
	ClientAccessEnabled = mode != "off"
	allowedOrigins = parseCSVEnv(os.Getenv("ALLOWED_ORIGINS"))
	TrustProxy = os.Getenv("TRUST_PROXY") == "true"

	if ClientAccessEnabled {
		log.Println("Client access: strict (Vue web UI origin validation)")
		if TrustProxy {
			log.Println("TRUST_PROXY enabled: honoring X-Forwarded-Host / X-Forwarded-Proto for origin checks")
		}
	}
}

// parseCSVEnv splits a comma-separated environment variable value into a
// trimmed, non-empty slice (nil if the input is blank).
func parseCSVEnv(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var values []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

// isProduction reports whether ENV or GO_ENV is set to "production",
// gating behaviors like the localhost CORS bypass and default-secret-key check.
func isProduction() bool {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("ENV")))
	goEnv := strings.ToLower(strings.TrimSpace(os.Getenv("GO_ENV")))
	return env == "production" || goEnv == "production"
}

// isPasswordStrongEnough enforces the minimum password length policy.
func isPasswordStrongEnough(password string) bool {
	return len(password) >= minPasswordLength
}

// isLocalhostHost reports whether host (with an optional :port) refers to
// the local machine, used to relax CORS for local frontend development.
func isLocalhostHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// requestHost returns the effective Host for this request, honoring
// X-Forwarded-Host when TRUST_PROXY is enabled.
func requestHost(r *http.Request) string {
	if TrustProxy {
		if host := r.Header.Get("X-Forwarded-Host"); host != "" {
			return strings.TrimSpace(strings.Split(host, ",")[0])
		}
	}
	return r.Host
}

// requestScheme returns "https" or "http" for this request, honoring
// X-Forwarded-Proto when TRUST_PROXY is enabled (otherwise based on r.TLS).
func requestScheme(r *http.Request) string {
	if TrustProxy {
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			return strings.TrimSpace(strings.Split(proto, ",")[0])
		}
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// sameOriginURL reconstructs this server's own origin (scheme://host), used
// to build OAuth redirect URLs and same-origin comparisons.
func sameOriginURL(r *http.Request) string {
	return requestScheme(r) + "://" + requestHost(r)
}

// corsOriginAllowed reports whether origin is allowed to make cross-origin
// requests: it must match an ALLOWED_ORIGINS entry, or (outside production)
// be localhost.
func corsOriginAllowed(origin string) bool {
	if origin == "" {
		return false
	}
	for _, allowed := range allowedOrigins {
		if allowedOriginEntryMatches(origin, allowed) {
			return true
		}
	}
	if !isProduction() {
		parsed, err := url.Parse(origin)
		if err == nil && isLocalhostHost(parsed.Host) {
			return true
		}
	}
	return false
}

// normalizeHost strips a port and brackets/lowercases a host for comparison.
func normalizeHost(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(strings.Trim(host, "[]"))
}

// allowedOriginEntryMatches reports whether origin matches one configured
// ALLOWED_ORIGINS entry, which may be a bare hostname or a full URL.
func allowedOriginEntryMatches(origin string, allowed string) bool {
	if origin == allowed {
		return true
	}
	parsedOrigin, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if strings.Contains(allowed, "://") {
		parsedAllowed, err := url.Parse(allowed)
		if err != nil {
			return false
		}
		return normalizeHost(parsedOrigin.Host) == normalizeHost(parsedAllowed.Host)
	}
	return normalizeHost(parsedOrigin.Host) == normalizeHost(allowed)
}

// allowedRefererMatches reports whether a Referer header value matches one
// configured allowed entry (used as a fallback when Origin is absent).
func allowedRefererMatches(referer string, allowed string) bool {
	if referer == allowed || strings.HasPrefix(referer, allowed+"/") {
		return true
	}
	parsedReferer, err := url.Parse(referer)
	if err != nil {
		return false
	}
	if strings.Contains(allowed, "://") {
		parsedAllowed, err := url.Parse(allowed)
		if err != nil {
			return false
		}
		return normalizeHost(parsedReferer.Host) == normalizeHost(parsedAllowed.Host)
	}
	return normalizeHost(parsedReferer.Host) == normalizeHost(allowed)
}

// originHostMatchesRequest reports whether origin's host equals this
// request's own effective host (helper for same-origin detection).
func originHostMatchesRequest(origin string, r *http.Request) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return normalizeHost(parsed.Host) == normalizeHost(requestHost(r))
}

// refererHostMatchesRequest reports whether a Referer header's host equals
// this request's own effective host.
func refererHostMatchesRequest(referer string, r *http.Request) bool {
	parsed, err := url.Parse(referer)
	if err != nil {
		return false
	}
	return normalizeHost(parsed.Host) == normalizeHost(requestHost(r))
}

// originMatchesAllowed is the full Origin-header decision: same-origin,
// same host, an ALLOWED_ORIGINS entry, or (outside production) localhost.
func originMatchesAllowed(origin string, r *http.Request) bool {
	if origin == sameOriginURL(r) {
		return true
	}
	if originHostMatchesRequest(origin, r) {
		return true
	}
	for _, allowed := range allowedOrigins {
		if allowedOriginEntryMatches(origin, allowed) {
			return true
		}
	}
	if !isProduction() {
		parsed, err := url.Parse(origin)
		if err == nil && isLocalhostHost(parsed.Host) {
			return true
		}
	}
	return false
}

// refererMatchesAllowed is the Referer-header equivalent of
// originMatchesAllowed, used when a browser omits the Origin header.
func refererMatchesAllowed(referer string, r *http.Request) bool {
	sameOrigin := sameOriginURL(r)
	if referer == sameOrigin || strings.HasPrefix(referer, sameOrigin+"/") {
		return true
	}
	if refererHostMatchesRequest(referer, r) {
		return true
	}
	for _, allowed := range allowedOrigins {
		if allowedRefererMatches(referer, allowed) {
			return true
		}
	}
	if !isProduction() {
		parsed, err := url.Parse(referer)
		if err == nil && isLocalhostHost(parsed.Host) {
			return true
		}
	}
	return false
}

// requestHostAllowed reports whether the request's own Host header is
// itself in the allowed set, used as a last resort for same-site fetches
// that send neither Origin nor Referer.
func requestHostAllowed(r *http.Request) bool {
	host := normalizeHost(requestHost(r))
	if host == "" {
		return false
	}
	if !isProduction() && isLocalhostHost(host) {
		return true
	}
	for _, allowed := range allowedOrigins {
		if strings.Contains(allowed, "://") {
			parsed, err := url.Parse(allowed)
			if err == nil && normalizeHost(parsed.Host) == host {
				return true
			}
		} else if normalizeHost(allowed) == host {
			return true
		}
	}
	if len(allowedOrigins) == 0 {
		return true
	}
	return false
}

// isWebOriginAllowed applies the full Origin/Referer/Sec-Fetch-Site fallback
// chain to decide whether a browser request came from an authorized web origin.
func isWebOriginAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin != "" && origin != "null" {
		return originMatchesAllowed(origin, r)
	}
	referer := r.Header.Get("Referer")
	if referer != "" {
		return refererMatchesAllowed(referer, r)
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "same-site":
		return requestHostAllowed(r)
	}
	return false
}

// isWebHTTPClientAllowed requires both the X-LightHouse-Client: web header
// and a valid browser origin, so raw script/curl access must instead
// authenticate with an API token.
func isWebHTTPClientAllowed(r *http.Request) bool {
	if strings.ToLower(r.Header.Get(headerLightHouseClient)) != clientHeaderWeb {
		return false
	}
	return isWebOriginAllowed(r)
}

// isClientAccessAllowed is the top-level gate applied to REST API requests
// (see clientAccessMiddleware); a no-op if CLIENT_ACCESS=off.
func isClientAccessAllowed(r *http.Request) bool {
	if !ClientAccessEnabled {
		return true
	}
	return isWebHTTPClientAllowed(r)
}

// isWSAccessAllowed is the CheckOrigin callback used by every WebSocket
// upgrade in the app; a no-op if CLIENT_ACCESS=off.
func isWSAccessAllowed(r *http.Request) bool {
	if !ClientAccessEnabled {
		return true
	}
	return isWebOriginAllowed(r)
}

// clientAccessConfig describes the current client-access policy for display
// via GET /api/config, so the frontend/debugging tools can see why a request
// might be rejected.
func clientAccessConfig() map[string]interface{} {
	return map[string]interface{}{
		"enabled": ClientAccessEnabled,
		"web": map[string]string{
			"client_header": headerLightHouseClient + "=web",
			"origin":        "Vue web UI — must match this server or ALLOWED_ORIGINS",
		},
	}
}

// newTestRequest builds an httptest request with the given headers set;
// used only by tests.
func newTestRequest(method, target string, headers map[string]string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return req
}

// clientAccessMiddleware enforces that /api and /ws requests originate from
// the LightHouse web app (or carry a valid API token), rejecting arbitrary
// third-party browser pages from riding on a logged-in user's session.
func clientAccessMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !ClientAccessEnabled {
				return next(c)
			}
			path := c.Request().URL.Path
			if !strings.HasPrefix(path, "/api") && !strings.HasPrefix(path, "/ws") {
				return next(c)
			}
			if c.Request().Method == http.MethodOptions {
				return next(c)
			}
			if strings.HasPrefix(path, "/ws") {
				if !isWSAccessAllowed(c.Request()) {
					return c.JSON(http.StatusForbidden, map[string]string{
						"error": "Access denied: WebSocket must originate from the web app",
					})
				}
				return next(c)
			}
			// PAT tokens bypass the X-LightHouse-Client header check for REST API calls,
			// but WebSocket origin validation above still applies.
			if strings.HasPrefix(c.Request().Header.Get("Authorization"), "Bearer lh_pat_") {
				return next(c)
			}
			if !isClientAccessAllowed(c.Request()) {
				return c.JSON(http.StatusForbidden, map[string]string{
					"error": "Access denied: request must originate from the web app",
				})
			}
			return next(c)
		}
	}
}

// isLimited reports whether key has hit max attempts within window,
// pruning any attempts older than window as a side effect.
func (rl *loginRateLimiter) isLimited(key string, max int, window time.Duration) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if rl.attempts == nil {
		rl.attempts = make(map[string][]time.Time)
	}
	now := time.Now()
	cutoff := now.Add(-window)
	var recent []time.Time
	for _, t := range rl.attempts[key] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	rl.attempts[key] = recent
	return len(recent) >= max
}

// recordFailure logs one failed login attempt for key at the current time.
func (rl *loginRateLimiter) recordFailure(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if rl.attempts == nil {
		rl.attempts = make(map[string][]time.Time)
	}
	rl.attempts[key] = append(rl.attempts[key], time.Now())
}

// clear resets key's failure history, called after a successful login.
func (rl *loginRateLimiter) clear(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.attempts, key)
}

// containerActionEnvAllowed checks the server-wide ALLOW_START/STOP/RESTART/
// DELETE env toggles for a given container action name.
func containerActionEnvAllowed(action string) bool {
	switch action {
	case "start":
		return CanStart
	case "stop":
		return CanStop
	case "restart":
		return CanRestart
	case "remove":
		return CanDelete
	default:
		return false
	}
}

// clampStaffActionPermissions forces each requested permission to false if
// the corresponding server-wide ALLOW_* toggle is disabled, ensuring no user
// (including admins configuring others) can grant more than the deployment allows.
func clampStaffActionPermissions(canStart, canStop, canRestart, canDelete, canShell bool) (bool, bool, bool, bool, bool) {
	if !CanStart {
		canStart = false
	}
	if !CanStop {
		canStop = false
	}
	if !CanRestart {
		canRestart = false
	}
	if !CanDelete {
		canDelete = false
	}
	if !AllowShell {
		canShell = false
	}
	return canStart, canStop, canRestart, canDelete, canShell
}

// staffContainerActionQuery returns the fixed, parameterized SQL used to
// check a non-admin user's per-action permission flag (never built from
// user input, so `action` cannot cause SQL injection).
func staffContainerActionQuery(action string) string {
	switch action {
	case "start":
		return "SELECT can_start FROM users WHERE id = ? AND is_active = 1"
	case "stop":
		return "SELECT can_stop FROM users WHERE id = ? AND is_active = 1"
	case "restart":
		return "SELECT can_restart FROM users WHERE id = ? AND is_active = 1"
	case "remove":
		return "SELECT can_delete FROM users WHERE id = ? AND is_active = 1"
	default:
		return ""
	}
}

// staffHasContainerActionPermission reports whether userID's account has the
// can_* flag required for action.
func staffHasContainerActionPermission(action string, userID int) (bool, error) {
	query := staffContainerActionQuery(action)
	if query == "" {
		return false, nil
	}

	var can bool
	err := db.DB.QueryRow(query, userID).Scan(&can)
	if err != nil {
		return false, err
	}
	return can, nil
}

// extractWSToken pulls the bearer JWT out of a WebSocket upgrade request.
// Browsers can't set custom headers during a WS handshake, so the token also
// travels via the "lighthouse-auth" Sec-WebSocket-Protocol entry as a fallback.
func extractWSToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}

	proto := r.Header.Get("Sec-WebSocket-Protocol")
	if proto == "" {
		return ""
	}

	parts := strings.Split(proto, ",")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "lighthouse-auth" && i+1 < len(parts) {
			return strings.TrimSpace(parts[i+1])
		}
	}
	return ""
}

const (
	tokenTypeAccess  = "access"
	tokenTypeRefresh = "refresh"
	accessTokenTTL   = 24 * time.Hour
	refreshTokenTTL  = 24 * time.Hour
)

// signUserToken signs claims as a JWT of the given tokenType (access or
// refresh), stamping fresh IssuedAt/ExpiresAt values for ttl.
func signUserToken(claims *UserClaims, tokenType string, ttl time.Duration) (string, error) {
	c := *claims
	c.TokenType = tokenType
	now := time.Now()
	c.RegisteredClaims = jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		IssuedAt:  jwt.NewNumericDate(now),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &c)
	return token.SignedString(SECRET_KEY)
}

// issueTokenPair signs a fresh access+refresh JWT pair for claims, returned
// to the client on login, OAuth callback, and token refresh.
func issueTokenPair(claims *UserClaims) (string, string, error) {
	access, err := signUserToken(claims, tokenTypeAccess, accessTokenTTL)
	if err != nil {
		return "", "", err
	}
	refresh, err := signUserToken(claims, tokenTypeRefresh, refreshTokenTTL)
	if err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

// refreshClaimsFromDB re-reads a user's live permissions/status from the
// database and overwrites the in-memory claims with them. This is what makes
// permission changes and account deactivation take effect immediately even
// though the JWT itself is still "valid" and unexpired — every request is
// re-authorized against current DB state, not just the token's snapshot.
func refreshClaimsFromDB(claims *UserClaims) error {
	var u db.User
	// Preload Team so we can merge team-level permissions
	if err := db.GormDB.Preload("Team").First(&u, claims.ID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("user not found")
		}
		return fmt.Errorf("database error: %v", err)
	}

	active := u.IsActive
	dbPwdVersion := u.PasswordVersion
	if u.PasswordVersion == 0 {
		dbPwdVersion = 1
	}
	isAdmin := u.IsAdmin
	// Start with user-level permissions
	canStart := u.CanStart
	canStop := u.CanStop
	canRestart := u.CanRestart
	canDelete := u.CanDelete
	canShell := u.CanShell
	isRestricted := u.IsRestrictedAccess
	allowedContainers := u.AllowedContainers
	changed := u.PasswordChanged

	// Merge team permissions via OR (same logic as login)
	if u.Team != nil {
		canStart = canStart || u.Team.CanStart
		canStop = canStop || u.Team.CanStop
		canRestart = canRestart || u.Team.CanRestart
		canDelete = canDelete || u.Team.CanDelete
		canShell = canShell || u.Team.CanShell
		if u.Team.AllowedContainers != "" {
			if allowedContainers == "" || allowedContainers == ".*" {
				allowedContainers = u.Team.AllowedContainers
			} else {
				allowedContainers = allowedContainers + "," + u.Team.AllowedContainers
			}
		}
	}

	if !active {
		return fmt.Errorf("account deactivated")
	}
	if claims.PasswordVersion != dbPwdVersion {
		return fmt.Errorf("session invalidated")
	}

	claims.IsAdmin = isAdmin
	claims.CanStart = canStart
	claims.CanStop = canStop
	claims.CanRestart = canRestart
	claims.CanDelete = canDelete
	claims.CanShell = canShell
	claims.IsRestrictedAccess = isRestricted
	claims.AllowedContainers = allowedContainers
	claims.IsActive = active
	claims.PasswordChanged = changed

	return nil
}

// parseUserToken verifies a JWT's signature (rejecting anything not signed
// with HS256, to prevent algorithm-confusion attacks) and decodes its claims.
func parseUserToken(tokenStr string) (*UserClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &UserClaims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("invalid signing method")
		}
		return SECRET_KEY, nil
	})
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return token.Claims.(*UserClaims), nil
}

// validateUserClaims refreshes claims from the DB and, if
// requirePasswordChanged is set, rejects users who still owe their mandatory
// first-login password change.
func validateUserClaims(claims *UserClaims, requirePasswordChanged bool) (*UserClaims, error) {
	if err := refreshClaimsFromDB(claims); err != nil {
		return nil, err
	}

	if requirePasswordChanged && !claims.PasswordChanged {
		return nil, fmt.Errorf("password change required")
	}

	return claims, nil
}

// validateUserToken parses and fully validates an access token for use on a
// normal API/WebSocket request.
func validateUserToken(tokenStr string) (*UserClaims, error) {
	claims, err := parseUserToken(tokenStr)
	if err != nil {
		return nil, err
	}
	if claims.TokenType == tokenTypeRefresh {
		return nil, fmt.Errorf("invalid token")
	}
	return validateUserClaims(claims, true)
}

// validateRefreshToken parses and validates a refresh token for use on
// /api/token/refresh.
func validateRefreshToken(tokenStr string) (*UserClaims, error) {
	claims, err := parseUserToken(tokenStr)
	if err != nil {
		return nil, err
	}
	if claims.TokenType != tokenTypeRefresh {
		return nil, fmt.Errorf("invalid token")
	}
	return validateUserClaims(claims, false)
}

// authenticateWS extracts and validates the JWT for an incoming WebSocket
// connection, before the HTTP connection is upgraded.
func authenticateWS(c echo.Context) (*UserClaims, error) {

	tokenStr := extractWSToken(c.Request())
	if tokenStr == "" {
		return nil, fmt.Errorf("missing token")
	}
	return validateUserToken(tokenStr)
}

// upgradeAuthenticatedWS performs the actual HTTP->WebSocket upgrade,
// echoing back the "lighthouse-auth" subprotocol the client requested.
func upgradeAuthenticatedWS(c echo.Context) (*websocket.Conn, error) {
	responseHeader := http.Header{}
	responseHeader.Set("Sec-WebSocket-Protocol", "lighthouse-auth")
	return upgrader.Upgrade(c.Response(), c.Request(), responseHeader)
}

// wsAuthError maps an authenticateWS error into a user-facing 401 JSON
// response with a friendlier message.
func wsAuthError(c echo.Context, err error) error {
	msg := "Authentication required"
	switch err.Error() {
	case "invalid token", "missing token":
		msg = err.Error()
	case "account deactivated":
		msg = "Account deactivated"
	case "session invalidated":
		msg = "Session invalidated"
	case "password change required":
		msg = "Password change required"
	}
	return c.JSON(http.StatusUnauthorized, map[string]string{"error": msg})
}

// securityHeadersMiddleware sets standard hardening headers (CSP,
// X-Frame-Options, nosniff, Referrer-Policy, Permissions-Policy, and HSTS
// when served over HTTPS) on every response.
func securityHeadersMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			h := c.Response().Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com data:; img-src 'self' data:; connect-src 'self' ws: wss:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
			if c.Scheme() == "https" {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			return next(c)
		}
	}
}
