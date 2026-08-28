package alerts

import (
	"crypto/tls"
	"errors"
	"net"
	"testing"
)

// TestDeliverEmail_SMTPNewClientError_Port465 fixes a gap in the existing
// TestDeliverEmailSMTPNewClientError test: that test never mocked tlsDial,
// so the real tls.Dial ran against the literal host "host" and failed at
// the dial step — the test still "passed" (any error satisfies its
// assertion) but never actually reached the smtpNewClient error branch it
// claims to cover. Here tlsDial is mocked to succeed (via tls.Client, which
// wraps a net.Conn lazily without performing a real handshake) so execution
// reaches — and this test genuinely exercises — the smtpNewClient failure path.
func TestDeliverEmail_SMTPNewClientError_Port465(t *testing.T) {
	origTLS := tlsDial
	origSMTP := smtpNewClient
	defer func() {
		tlsDial = origTLS
		smtpNewClient = origSMTP
	}()

	tlsDial = func(network, addr string, config *tls.Config) (*tls.Conn, error) {
		return tls.Client(&mockConn{}, &tls.Config{InsecureSkipVerify: true}), nil
	}
	smtpNewClient = func(conn net.Conn, host string) (smtpClient, error) {
		return nil, errors.New("mock smtp new client error")
	}

	err := DeliverEmail("host", 465, "user", "pass", "from", []string{"to"}, NotificationPayload{})
	if err == nil {
		t.Fatal("expected error when smtpNewClient fails on the port-465 implicit-TLS path")
	}
}
