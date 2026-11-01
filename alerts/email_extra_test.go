package alerts

import (
	"crypto/tls"
	"errors"
	"net"
	"net/smtp"
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestSendRawEmailSMTPAndTLS(t *testing.T) {
	originalTLS, originalSMTP, originalSend := tlsDial, smtpNewClient, smtpSendMail
	t.Cleanup(func() {
		tlsDial, smtpNewClient, smtpSendMail = originalTLS, originalSMTP, originalSend
	})
	message := []byte("Subject: fixture\r\n\r\nInvitation body")
	called := false
	smtpSendMail = func(address string, auth smtp.Auth, from string, recipients []string, body []byte) error {
		called = true
		assert.Equal(t, "mail.example:587", address)
		assert.NotNil(t, auth)
		assert.Equal(t, "sender@example.test", from)
		assert.Equal(t, []string{"first@example.test", "second@example.test"}, recipients)
		assert.Equal(t, message, body)
		return nil
	}
	assert.NoError(t, SendRawEmail("mail.example", 587, "sender", "password", "sender@example.test", []string{"first@example.test", "second@example.test"}, message))
	assert.True(t, called)
	called = false
	assert.Error(t, SendRawEmail("mail.example", 587, "", "", "sender@example.test", nil, message))
	assert.False(t, called)
	tlsDial = func(network, address string, config *tls.Config) (*tls.Conn, error) {
		assert.Equal(t, "tcp", network)
		assert.Equal(t, "mail.example:465", address)
		assert.Equal(t, "mail.example", config.ServerName)
		assert.False(t, config.InsecureSkipVerify)
		return tls.Client(&mockConn{}, config), nil
	}
	smtpNewClient = func(net.Conn, string) (smtpClient, error) {
		return &mockSMTPClient{wc: &mockWriteCloser{}}, nil
	}
	assert.NoError(t, SendRawEmail("mail.example", 465, "sender", "password", "sender@example.test", []string{"first@example.test", "second@example.test"}, message))
	assert.NoError(t, SendRawEmail("mail.example", 465, "", "", "sender@example.test", []string{"first@example.test"}, message))
}

func TestSendRawEmailReportsDeliveryFailures(t *testing.T) {
	originalTLS, originalSMTP, originalSend := tlsDial, smtpNewClient, smtpSendMail
	t.Cleanup(func() {
		tlsDial, smtpNewClient, smtpSendMail = originalTLS, originalSMTP, originalSend
	})
	failure := errors.New("fixture delivery failure")
	smtpSendMail = func(string, smtp.Auth, string, []string, []byte) error { return failure }
	assert.ErrorIs(t, SendRawEmail("host", 587, "", "", "from", []string{"to"}, nil), failure)
	tlsDial = func(string, string, *tls.Config) (*tls.Conn, error) { return nil, failure }
	assert.ErrorIs(t, SendRawEmail("host", 465, "", "", "from", []string{"to"}, nil), failure)
	tlsDial = func(_ string, _ string, config *tls.Config) (*tls.Conn, error) {
		return tls.Client(&mockConn{}, config), nil
	}
	smtpNewClient = func(net.Conn, string) (smtpClient, error) { return nil, failure }
	assert.ErrorIs(t, SendRawEmail("host", 465, "", "", "from", []string{"to"}, nil), failure)
	for _, test := range []struct {
		name   string
		client *mockSMTPClient
	}{
		{"authentication", &mockSMTPClient{authErr: failure}},
		{"sender", &mockSMTPClient{mailErr: failure}},
		{"recipient", &mockSMTPClient{rcptErr: failure}},
		{"data", &mockSMTPClient{dataErr: failure}},
		{"write", &mockSMTPClient{wc: &mockWriteCloser{writeErr: failure}}},
		{"close", &mockSMTPClient{wc: &mockWriteCloser{closeErr: failure}}},
		{"quit", &mockSMTPClient{wc: &mockWriteCloser{}, quitErr: failure}},
	} {
		t.Run(test.name, func(t *testing.T) {
			smtpNewClient = func(net.Conn, string) (smtpClient, error) { return test.client, nil }
			assert.ErrorIs(t, SendRawEmail("host", 465, "user", "password", "from", []string{"to"}, []byte("message")), failure)
		})
	}
}
