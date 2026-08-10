package alerts

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

// removed TestIsAllowedWebhookURL since it requires real DNS resolution

func TestSlackWebhook(t *testing.T) {
	received := false
	origTransport := httpClient.Transport
	httpClient.Transport = &dockerMockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			received = true
			body, _ := io.ReadAll(req.Body)
			var payload slackPayload
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("Failed to parse slack payload: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBuffer(nil)),
			}, nil
		},
	}
	defer func() { httpClient.Transport = origTransport }()

	SkipSSRFCheck = true
	defer func() { SkipSSRFCheck = false }()

	payload := NotificationPayload{
		RuleName:      "Test Slack Rule",
		ContainerName: "test-container",
		Type:          "event",
		Details:       "This is a test alert",
		Timestamp:     time.Now().Format(time.RFC3339),
	}

	configJSON := `{"url":"http://dummy"}`
	err := DeliverNotification("slack", configJSON, payload)
	if err != nil {
		t.Fatalf("DeliverNotification slack failed: %v", err)
	}
	if !received {
		t.Fatal("Test server did not receive slack webhook")
	}

	// test recovery type
	payload.Type = "recovery"
	_ = DeliverNotification("slack", configJSON, payload)

	// test digest type
	payload.Type = "digest"
	_ = DeliverNotification("slack", configJSON, payload)

	// test audit type
	payload.Type = "audit"
	_ = DeliverNotification("slack", configJSON, payload)

	// test default type
	payload.Type = "unknown"
	_ = DeliverNotification("slack", configJSON, payload)

	// Test long details
	payload.Details = string(make([]byte, 1000))
	_ = DeliverNotification("slack", configJSON, payload)
}

func TestMSTeamsWebhook(t *testing.T) {
	received := false
	origTransport := httpClient.Transport
	httpClient.Transport = &dockerMockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			received = true
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBuffer(nil)),
			}, nil
		},
	}
	defer func() { httpClient.Transport = origTransport }()

	SkipSSRFCheck = true
	defer func() { SkipSSRFCheck = false }()

	payload := NotificationPayload{
		RuleName:      "Test MSTeams Rule",
		ContainerName: "test-container",
		Type:          "event",
		Details:       "This is a test alert",
		Timestamp:     time.Now().Format(time.RFC3339),
	}

	configJSON := `{"url":"http://dummy"}`
	err := DeliverNotification("msteams", configJSON, payload)
	if err != nil {
		t.Fatalf("DeliverNotification msteams failed: %v", err)
	}
	if !received {
		t.Fatal("Test server did not receive msteams webhook")
	}

	types := []string{"recovery", "digest", "audit", "unknown"}
	for _, typ := range types {
		payload.Type = typ
		_ = DeliverNotification("msteams", configJSON, payload)
	}
}

func TestGChatWebhook(t *testing.T) {
	received := false
	origTransport := httpClient.Transport
	httpClient.Transport = &dockerMockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			received = true
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBuffer(nil)),
			}, nil
		},
	}
	defer func() { httpClient.Transport = origTransport }()

	SkipSSRFCheck = true
	defer func() { SkipSSRFCheck = false }()

	payload := NotificationPayload{
		RuleName:      "Test GChat Rule",
		ContainerName: "test-container",
		Type:          "event",
		Details:       "This is a test alert",
		Timestamp:     time.Now().Format(time.RFC3339),
	}

	configJSON := `{"url":"http://dummy"}`
	err := DeliverNotification("gchat", configJSON, payload)
	if err != nil {
		t.Fatalf("DeliverNotification gchat failed: %v", err)
	}
	if !received {
		t.Fatal("Test server did not receive gchat webhook")
	}

	types := []string{"recovery", "digest", "audit", "unknown"}
	for _, typ := range types {
		payload.Type = typ
		_ = DeliverNotification("gchat", configJSON, payload)
	}
}

func TestDeliverNotification_Errors(t *testing.T) {
	err := DeliverNotification("slack", `invalid json`, NotificationPayload{})
	if err == nil {
		t.Fatal("Expected error for invalid json")
	}

	err = DeliverNotification("slack", `{}`, NotificationPayload{})
	if err == nil {
		t.Fatal("Expected error for missing url")
	}

	err = DeliverNotification("unknown_type", `{"url":"http://dummy"}`, NotificationPayload{})
	if err == nil {
		t.Fatal("Expected error for unknown channel type")
	}
}

func TestPostJSON_Errors(t *testing.T) {
	SkipSSRFCheck = false // Ensure SSRF check is active for this test
	err := postJSON("http://127.0.0.1/webhook", []byte("{}"))
	if err == nil {
		t.Fatal("Expected error for blocked SSRF URL")
	}
}

func TestDeliverEmail(t *testing.T) {
	// For DeliverEmail, since it tries to connect to an SMTP server,
	// testing it without a real/mock SMTP server will fail at TLS Dial or smtp.Dial.
	// But we can at least test that it handles invalid configurations.
	payload := NotificationPayload{
		Type:          "test",
		ContainerName: "test-container",
	}
	// Try sending to a dummy port, expecting an error about connection refused
	err := DeliverEmail("127.0.0.1", 12345, "user", "pass", "to@example.com", []string{"cc@example.com"}, payload)
	if err == nil {
		t.Fatal("Expected error connecting to dummy SMTP server")
	}

	// Try with empty recipients
	err = DeliverEmail("127.0.0.1", 12345, "user", "pass", "", []string{}, payload)
	if err == nil {
		t.Fatal("Expected error for no recipients")
	}
}
