package cluster

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"lighthouse/db"

	"github.com/stretchr/testify/assert"
)

func TestProxyRequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test_token", r.Header.Get("Authorization"))
		assert.Equal(t, "/api/test", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "ok"}`))
	}))
	defer ts.Close()

	node := db.Node{
		Address: ts.URL,
		Token:   "test_token",
	}

	b, err := ProxyRequest(node, "GET", "/api/test", nil)
	assert.NoError(t, err)
	assert.Contains(t, string(b), `"status": "ok"`)
}

func TestFetchSpokeContainers(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/containers", r.URL.Path)
		
		containers := []map[string]interface{}{
			{"Id": "123", "Names": []string{"/test1"}},
			{"Id": "456", "Names": []string{"/test2"}},
		}
		b, _ := json.Marshal(containers)
		w.WriteHeader(http.StatusOK)
		w.Write(b)
	}))
	defer ts.Close()

	node := db.Node{
		ID:      1,
		Name:    "spoke1",
		Address: ts.URL,
		Token:   "test_token",
	}

	containers, err := FetchSpokeContainers(node)
	assert.NoError(t, err)
	assert.Len(t, containers, 2)
	assert.Equal(t, "123", containers[0]["Id"])
	assert.Equal(t, uint(1), containers[0]["node_id"])
	assert.Equal(t, "spoke1", containers[0]["node_name"])
}
