package cluster

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"testing"

	"lighthouse/db"

	"github.com/stretchr/testify/assert"
)

type mockTransport struct {
	fn func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.fn(req)
}

func TestProxyRequest(t *testing.T) {
	node := db.Node{
		Address: "http://dummy",
		Token:   "test_token",
	}

	t.Run("Success", func(t *testing.T) {
		httpClient.Transport = &mockTransport{
			fn: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       ioutil.NopCloser(bytes.NewBufferString(`{"status": "ok"}`)),
				}, nil
			},
		}

		b, err := ProxyRequest(node, "GET", "/api/test", nil)
		assert.NoError(t, err)
		assert.Contains(t, string(b), `"status": "ok"`)
	})

	t.Run("NewRequestError", func(t *testing.T) {
		_, err := ProxyRequest(node, " \x00", "/api/test", nil)
		assert.Error(t, err)
	})

	t.Run("DoError", func(t *testing.T) {
		httpClient.Transport = &mockTransport{
			fn: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("network error")
			},
		}
		_, err := ProxyRequest(node, "GET", "/api/test", nil)
		assert.Error(t, err)
	})

	t.Run("StatusCodeError", func(t *testing.T) {
		httpClient.Transport = &mockTransport{
			fn: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 500,
					Body:       ioutil.NopCloser(bytes.NewBufferString(`internal error`)),
				}, nil
			},
		}
		_, err := ProxyRequest(node, "GET", "/api/test", nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "spoke error")
	})
}

func TestFetchSpokeContainers(t *testing.T) {
	node := db.Node{
		ID:      1,
		Name:    "spoke1",
		Address: "http://dummy",
		Token:   "test_token",
	}

	t.Run("Success", func(t *testing.T) {
		httpClient.Transport = &mockTransport{
			fn: func(req *http.Request) (*http.Response, error) {
				containers := []map[string]interface{}{
					{"Id": "123", "Names": []string{"/test1"}},
				}
				b, _ := json.Marshal(containers)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       ioutil.NopCloser(bytes.NewBuffer(b)),
				}, nil
			},
		}

		containers, err := FetchSpokeContainers(node)
		assert.NoError(t, err)
		assert.Len(t, containers, 1)
		assert.Equal(t, uint(1), containers[0]["node_id"])
		assert.Equal(t, "spoke1", containers[0]["node_name"])
	})

	t.Run("ProxyRequestError", func(t *testing.T) {
		httpClient.Transport = &mockTransport{
			fn: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("network error")
			},
		}
		_, err := FetchSpokeContainers(node)
		assert.Error(t, err)
	})

	t.Run("JSONUnmarshalError", func(t *testing.T) {
		httpClient.Transport = &mockTransport{
			fn: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       ioutil.NopCloser(bytes.NewBufferString(`invalid json`)),
				}, nil
			},
		}
		_, err := FetchSpokeContainers(node)
		assert.Error(t, err)
	})
}
