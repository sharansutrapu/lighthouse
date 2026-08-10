package alerts

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/smtp"
	"testing"
	"time"
)

func TestDeliveryJSONErrors(t *testing.T) {
	oldJSON := jsonMarshal
	defer func() { jsonMarshal = oldJSON }()
	jsonMarshal = func(v interface{}) ([]byte, error) {
		return nil, errors.New("mock json error")
	}

	_ = sendSlackWebhook("http://dummy", NotificationPayload{})
	_ = sendMSTeamsWebhook("http://dummy", NotificationPayload{})
	_ = sendGChatWebhook("http://dummy", NotificationPayload{})
	_ = sendGenericWebhook("http://dummy", NotificationPayload{})
}

func TestDeliveryLookupError(t *testing.T) {
	oldLookup := netLookupHost
	defer func() { netLookupHost = oldLookup }()
	netLookupHost = func(host string) (addrs []string, err error) {
		return nil, errors.New("mock dns error")
	}

	if isAllowedWebhookURL("http://dummy") {
		t.Error("expected false for dns error")
	}

	// Test localhost
	if isAllowedWebhookURL("http://127.0.0.1") {
		t.Error("expected false for localhost")
	}

	// Test invalid scheme
	if isAllowedWebhookURL("ftp://dummy") {
		t.Error("expected false for ftp")
	}
}

func TestDeliverEmailSMTPNewClientError(t *testing.T) {
	oldSMTP := smtpNewClient
	defer func() { smtpNewClient = oldSMTP }()
	smtpNewClient = func(conn net.Conn, host string) (smtpClient, error) {
		return nil, errors.New("mock smtp error")
	}

	err := DeliverEmail("host", 465, "user", "pass", "from", []string{"to"}, NotificationPayload{})
	if err == nil {
		t.Error("expected error for mock smtp error")
	}
}

type mockWriteCloser struct {
	writeErr error
	closeErr error
}

func (m *mockWriteCloser) Write(p []byte) (n int, err error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	return len(p), nil
}

func (m *mockWriteCloser) Close() error {
	return m.closeErr
}

type mockSMTPClient struct {
	authErr error
	mailErr error
	rcptErr error
	dataErr error
	quitErr error
	wc      *mockWriteCloser
}

func (m *mockSMTPClient) Auth(a smtp.Auth) error { return m.authErr }
func (m *mockSMTPClient) Mail(from string) error { return m.mailErr }
func (m *mockSMTPClient) Rcpt(to string) error   { return m.rcptErr }
func (m *mockSMTPClient) Data() (io.WriteCloser, error) {
	if m.dataErr != nil {
		return nil, m.dataErr
	}
	return m.wc, nil
}
func (m *mockSMTPClient) Quit() error  { return m.quitErr }
func (m *mockSMTPClient) Close() error { return nil }

func TestDeliverEmailDetailedErrors(t *testing.T) {
	oldSMTP := smtpNewClient
	defer func() { smtpNewClient = oldSMTP }()

	oldTLS := tlsDial
	defer func() { tlsDial = oldTLS }()
	tlsDial = func(network, addr string, config *tls.Config) (*tls.Conn, error) {
		return tls.Client(&mockConn{}, config), nil
	}

	cases := []struct {
		name   string
		client *mockSMTPClient
	}{
		{"auth err", &mockSMTPClient{authErr: errors.New("err")}},
		{"mail err", &mockSMTPClient{mailErr: errors.New("err")}},
		{"rcpt err", &mockSMTPClient{rcptErr: errors.New("err")}},
		{"data err", &mockSMTPClient{dataErr: errors.New("err")}},
		{"write err", &mockSMTPClient{wc: &mockWriteCloser{writeErr: errors.New("err")}}},
		{"close err", &mockSMTPClient{wc: &mockWriteCloser{closeErr: errors.New("err")}}},
		{"quit err", &mockSMTPClient{wc: &mockWriteCloser{}, quitErr: errors.New("err")}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			smtpNewClient = func(conn net.Conn, host string) (smtpClient, error) {
				return c.client, nil
			}
			DeliverEmail("host", 465, "user", "pass", "from", []string{"to"}, NotificationPayload{})
		})
	}

	// Test empty TO
	smtpNewClient = func(conn net.Conn, host string) (smtpClient, error) {
		return &mockSMTPClient{wc: &mockWriteCloser{}}, nil
	}
	DeliverEmail("host", 465, "user", "pass", "from", []string{}, NotificationPayload{})
	DeliverEmail("host", 465, "user", "pass", "from", nil, NotificationPayload{})
}

type mockConn struct{}

func (m *mockConn) Read(b []byte) (n int, err error)   { return 0, nil }
func (m *mockConn) Write(b []byte) (n int, err error)  { return len(b), nil }
func (m *mockConn) Close() error                       { return nil }
func (m *mockConn) LocalAddr() net.Addr                { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }
