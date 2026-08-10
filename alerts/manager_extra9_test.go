package alerts

import (
	"net/http"
	"testing"
)

type dockerMockTransport struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (t *dockerMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.roundTripFunc(req)
}

func setupTestDB(t *testing.T) {
	// dummy so we don't need to rewrite all files right now
	setupTestManager(t)
}
