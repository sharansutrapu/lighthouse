package alerts

import (
	"net"
	"testing"
)

// TestIsAllowedWebhookURL_AdditionalBranches closes gaps left by existing
// tests, which only ever exercised the "blocked" paths: a malformed URL that
// actually fails url.Parse (net/url is very lenient — most strings "parse"
// successfully as a relative reference), a genuinely public raw IP (the
// "allowed" return true branch was never hit), and a hostname that resolves
// to one or more addresses successfully (every existing netLookupHost
// override only ever returned an error, so the resolution loop itself —
// including its own allow/deny outcomes — was never executed).
func TestIsAllowedWebhookURL_AdditionalBranches(t *testing.T) {
	t.Run("url.Parse itself fails (invalid percent-encoding)", func(t *testing.T) {
		if isAllowedWebhookURL("http://%zz") {
			t.Error("expected false for a URL that fails to parse")
		}
	})

	t.Run("public raw IP literal is allowed", func(t *testing.T) {
		if !isAllowedWebhookURL("http://8.8.8.8") {
			t.Error("expected true for a public raw IP literal")
		}
	})

	origLookup := netLookupHost
	defer func() { netLookupHost = origLookup }()

	t.Run("hostname resolves to a public IP -> allowed", func(t *testing.T) {
		netLookupHost = func(host string) ([]string, error) {
			return []string{"8.8.8.8"}, nil
		}
		if !isAllowedWebhookURL("http://public-host.example") {
			t.Error("expected true when resolved address is public")
		}
	})

	t.Run("hostname resolves to a private IP -> blocked (DNS rebinding)", func(t *testing.T) {
		netLookupHost = func(host string) ([]string, error) {
			return []string{"10.0.0.5"}, nil
		}
		if isAllowedWebhookURL("http://rebind.example") {
			t.Error("expected false when a resolved address is private")
		}
	})

	t.Run("hostname resolves to an unparseable address -> blocked", func(t *testing.T) {
		netLookupHost = func(host string) ([]string, error) {
			return []string{"not-an-ip"}, nil
		}
		if isAllowedWebhookURL("http://garbage-dns.example") {
			t.Error("expected false when a resolved address string is not a valid IP")
		}
	})
}

// TestDefaultSMTPNewClient covers the default smtpNewClient implementation,
// which every DeliverEmail test overrides — leaving the real closure that
// wraps smtp.NewClient completely unexercised.
func TestDefaultSMTPNewClient(t *testing.T) {
	_, _ = smtpNewClient(&mockConn{}, "smtp.example.com")
}

var _ net.Conn = (*mockConn)(nil)
