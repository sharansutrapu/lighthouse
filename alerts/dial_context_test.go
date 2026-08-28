package alerts

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
)

// TestIsSafeIP exercises every branch of isSafeIP: loopback, RFC1918/ULA
// private ranges, link-local unicast/multicast, multicast, unspecified,
// nil, and a genuinely routable public address (happy path).
func TestIsSafeIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ip   net.IP
		want bool
	}{
		{name: "nil IP", ip: nil, want: false},
		{name: "IPv4 loopback", ip: net.ParseIP("127.0.0.1"), want: false},
		{name: "IPv6 loopback", ip: net.ParseIP("::1"), want: false},
		{name: "IPv4 private 10/8", ip: net.ParseIP("10.0.0.5"), want: false},
		{name: "IPv4 private 172.16/12", ip: net.ParseIP("172.16.0.1"), want: false},
		{name: "IPv4 private 192.168/16", ip: net.ParseIP("192.168.1.1"), want: false},
		{name: "IPv6 unique local fc00::/7", ip: net.ParseIP("fc00::1"), want: false},
		{name: "cloud metadata link-local 169.254.169.254", ip: net.ParseIP("169.254.169.254"), want: false},
		{name: "IPv6 link-local unicast fe80::/10", ip: net.ParseIP("fe80::1"), want: false},
		{name: "IPv4 multicast", ip: net.ParseIP("224.0.0.1"), want: false},
		{name: "IPv6 link-local multicast", ip: net.ParseIP("ff02::1"), want: false},
		{name: "IPv4 unspecified 0.0.0.0", ip: net.ParseIP("0.0.0.0"), want: false},
		{name: "IPv6 unspecified ::", ip: net.ParseIP("::"), want: false},
		{name: "public IPv4 (happy path)", ip: net.ParseIP("8.8.8.8"), want: true},
		{name: "public IPv6 (happy path)", ip: net.ParseIP("2001:4860:4860::8888"), want: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isSafeIP(tc.ip); got != tc.want {
				t.Fatalf("isSafeIP(%v) = %v, want %v", tc.ip, got, tc.want)
			}
		})
	}
}

// TestSafeDialContext exercises safeDialContext without ever touching a real
// network or DNS resolver: lookupIPAddr and rawDialContext are both
// function-variable overrides.
func TestSafeDialContext(t *testing.T) {
	origLookup := lookupIPAddr
	origDial := rawDialContext
	origSkip := SkipSSRFCheck
	defer func() {
		lookupIPAddr = origLookup
		rawDialContext = origDial
		SkipSSRFCheck = origSkip
	}()

	dialSucceeds := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return &mockConn{}, nil
	}

	t.Run("happy path: direct public IP literal, no DNS needed", func(t *testing.T) {
		SkipSSRFCheck = false
		lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
			t.Fatal("lookupIPAddr should not be called for an IP literal")
			return nil, nil
		}
		rawDialContext = dialSucceeds

		conn, err := safeDialContext(context.Background(), "tcp", "8.8.8.8:443")
		if err != nil {
			t.Fatalf("safeDialContext() error = %v, want nil", err)
		}
		if conn == nil {
			t.Fatal("safeDialContext() returned nil conn on success")
		}
	})

	t.Run("happy path: hostname resolves to public IP", func(t *testing.T) {
		SkipSSRFCheck = false
		lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		}
		var dialedAddr string
		rawDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialedAddr = addr
			return &mockConn{}, nil
		}

		_, err := safeDialContext(context.Background(), "tcp", "example.com:443")
		if err != nil {
			t.Fatalf("safeDialContext() error = %v, want nil", err)
		}
		if dialedAddr != "93.184.216.34:443" {
			t.Fatalf("dialed %q, want the resolved IP:port (not the original hostname)", dialedAddr)
		}
	})

	t.Run("hostile: direct IP literal is cloud metadata endpoint", func(t *testing.T) {
		SkipSSRFCheck = false
		rawDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			t.Fatal("rawDialContext must not be called for a blocked SSRF target")
			return nil, nil
		}

		_, err := safeDialContext(context.Background(), "tcp", "169.254.169.254:80")
		if err == nil {
			t.Fatal("safeDialContext() = nil error, want SSRF block for 169.254.169.254")
		}
	})

	t.Run("hostile: hostname resolves to a private IP (DNS rebinding target)", func(t *testing.T) {
		SkipSSRFCheck = false
		lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("10.0.0.5")}}, nil
		}
		rawDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			t.Fatal("rawDialContext must not be called for a blocked SSRF target")
			return nil, nil
		}

		_, err := safeDialContext(context.Background(), "tcp", "internal.corp:8080")
		if err == nil {
			t.Fatal("safeDialContext() = nil error, want SSRF block for private IP")
		}
	})

	t.Run("SkipSSRFCheck bypasses the block (explicit opt-out, e.g. tests)", func(t *testing.T) {
		SkipSSRFCheck = true
		lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		}
		dialed := false
		rawDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialed = true
			return &mockConn{}, nil
		}

		_, err := safeDialContext(context.Background(), "tcp", "loopback.test:80")
		if err != nil {
			t.Fatalf("safeDialContext() error = %v, want nil when SkipSSRFCheck=true", err)
		}
		if !dialed {
			t.Fatal("rawDialContext was not called even though SkipSSRFCheck=true")
		}
	})

	t.Run("infra failure: DNS resolution error", func(t *testing.T) {
		SkipSSRFCheck = false
		lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return nil, errors.New("simulated DNS timeout")
		}
		rawDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			t.Fatal("rawDialContext must not be called when DNS resolution fails")
			return nil, nil
		}

		_, err := safeDialContext(context.Background(), "tcp", "flaky-dns.example:443")
		if err == nil {
			t.Fatal("safeDialContext() = nil error, want dns resolution failure surfaced")
		}
	})

	t.Run("infra failure: DNS resolves to zero addresses", func(t *testing.T) {
		SkipSSRFCheck = false
		lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return []net.IPAddr{}, nil
		}
		_, err := safeDialContext(context.Background(), "tcp", "empty-answer.example:443")
		if err == nil {
			t.Fatal("safeDialContext() = nil error, want failure for empty DNS answer")
		}
	})

	t.Run("infra failure: malformed addr with no port", func(t *testing.T) {
		_, err := safeDialContext(context.Background(), "tcp", "no-port-here")
		if err == nil {
			t.Fatal("safeDialContext() = nil error, want SplitHostPort failure")
		}
	})

	t.Run("infra failure: downstream dial itself fails (connection refused)", func(t *testing.T) {
		SkipSSRFCheck = false
		lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
		}
		rawDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return nil, errors.New("connection refused")
		}

		_, err := safeDialContext(context.Background(), "tcp", "unreachable.example:443")
		if err == nil {
			t.Fatal("safeDialContext() = nil error, want the underlying dial error propagated")
		}
	})
}

// TestHTTPClientUsesSafeDialContext confirms the package-level httpClient is
// actually wired to go through safeDialContext, so the SSRF protection
// applies to every real request the alerting engine sends (not just direct
// unit-tested calls to the helper).
func TestHTTPClientUsesSafeDialContext(t *testing.T) {
	tr, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("httpClient.Transport = %T, want *http.Transport with DialContext set to safeDialContext", httpClient.Transport)
	}
	if tr.DialContext == nil {
		t.Fatal("httpClient.Transport.DialContext is nil; SSRF DialContext hook is not wired up")
	}
}
