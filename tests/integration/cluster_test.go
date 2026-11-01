//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"lighthouse/db"
)

const fixturePassword = "Integration-test-password-2026"

type fixtureAPI struct {
	baseURL string
	token   string
	client  *http.Client
}

type fixtureContainer struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	NodeID       string          `json:"node_id"`
	IsRemote     bool            `json:"is_remote"`
	CPU          float64         `json:"cpu"`
	Memory       int64           `json:"memory"`
	Capabilities map[string]bool `json:"capabilities"`
}

type fixtureWorkload struct {
	engine string
	node   string
	id     string
	name   string
}

func (api *fixtureAPI) request(t *testing.T, method, path string, payload interface{}) (int, []byte) {
	t.Helper()
	var body io.Reader
	contentType := "application/json"
	if form, ok := payload.(url.Values); ok {
		body = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	} else if payload != nil {
		encoded, err := json.Marshal(payload)
		require.NoError(t, err)
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, api.baseURL+path, body)
	require.NoError(t, err)
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("X-LightHouse-Client", "web")
	request.Header.Set("Origin", api.baseURL)
	if api.token != "" {
		request.Header.Set("Authorization", "Bearer "+api.token)
	}
	response, err := api.client.Do(request)
	require.NoError(t, err, "%s %s", method, path)
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	require.NoError(t, err)
	return response.StatusCode, data
}

func (api *fixtureAPI) login(t *testing.T, username, password string) {
	t.Helper()
	status, data := api.request(t, http.MethodPost, "/api/token", url.Values{"username": {username}, "password": {password}})
	require.Equal(t, http.StatusOK, status, "login failed for fixture user %s", username)
	var response struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(data, &response))
	require.NotEmpty(t, response.AccessToken)
	api.token = response.AccessToken
}

func authenticatedFixture(t *testing.T, baseURL string) *fixtureAPI {
	t.Helper()
	api := &fixtureAPI{baseURL: baseURL, client: &http.Client{Timeout: 90 * time.Second}}
	status, data := api.request(t, http.MethodPost, "/api/token", url.Values{"username": {"admin"}, "password": {"admin123"}})
	if status == http.StatusUnauthorized {
		api.login(t, "admin", fixturePassword)
		return api
	}
	require.Equal(t, http.StatusOK, status)
	var credentials struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(data, &credentials))
	require.NotEmpty(t, credentials.AccessToken)
	api.token = credentials.AccessToken
	status, _ = api.request(t, http.MethodPost, "/api/user/change-password", url.Values{"password": {fixturePassword}})
	require.Equal(t, http.StatusOK, status)
	api.login(t, "admin", fixturePassword)
	return api
}

func engineCommand(t *testing.T, engine string, arguments ...string) string {
	t.Helper()
	require.Contains(t, []string{"engine-hub", "engine-one", "engine-two"}, engine, "refusing to access a non-test Docker daemon")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "docker", append([]string{"--host", "tcp://" + engine + ":2375"}, arguments...)...)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "test daemon %s: %s", engine, output)
	return strings.TrimSpace(string(output))
}

func fixtureDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	connection, err := gorm.Open(postgres.Open("host=postgres user=lighthouse_test password=integration-only-database-password dbname=lighthouse_test port=5432 sslmode=disable"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	pool, err := connection.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = pool.Close() })
	return connection
}

func (api *fixtureAPI) containers(t *testing.T) []fixtureContainer {
	t.Helper()
	status, data := api.request(t, http.MethodGet, "/api/containers", nil)
	require.Equal(t, http.StatusOK, status)
	var containers []fixtureContainer
	require.NoError(t, json.Unmarshal(data, &containers))
	return containers
}

func (api *fixtureAPI) socket(t *testing.T, path string) *websocket.Conn {
	t.Helper()
	header := http.Header{"Authorization": {"Bearer " + api.token}, "Origin": {api.baseURL}}
	connection, response, err := websocket.DefaultDialer.Dial(strings.Replace(api.baseURL, "http://", "ws://", 1)+path, header)
	if response != nil {
		defer response.Body.Close()
	}
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	require.NoError(t, connection.SetReadDeadline(time.Now().Add(15*time.Second)))
	return connection
}

func restrictedFixture(t *testing.T, hub *fixtureAPI) (*fixtureAPI, uint) {
	t.Helper()
	status, data := hub.request(t, http.MethodGet, "/api/admin/role_templates", nil)
	require.Equal(t, http.StatusOK, status)
	var roles []db.RoleTemplate
	require.NoError(t, json.Unmarshal(data, &roles))
	var roleID uint
	for _, role := range roles {
		if role.Name == "Read-Only Observer" {
			roleID = role.ID
		}
	}
	require.NotZero(t, roleID)
	username := fmt.Sprintf("verify-reader-%d", time.Now().UnixNano())
	status, _ = hub.request(t, http.MethodPost, "/api/admin/users", url.Values{
		"username": {username}, "password": {fixturePassword}, "authMethod": {"local"},
		"role_template_id": {fmt.Sprint(roleID)}, "is_restricted": {"true"}, "allowed_containers": {"^verify-one-web$"},
	})
	require.Equal(t, http.StatusCreated, status)
	reader := &fixtureAPI{baseURL: hub.baseURL, client: hub.client}
	reader.login(t, username, fixturePassword)
	status, _ = reader.request(t, http.MethodGet, "/api/containers", nil)
	require.Equal(t, http.StatusForbidden, status, "password change must be enforced before reading inventory")
	status, _ = reader.request(t, http.MethodPost, "/api/user/change-password", url.Values{"password": {fixturePassword + "-reader"}})
	require.Equal(t, http.StatusOK, status)
	reader.login(t, username, fixturePassword+"-reader")
	status, data = reader.request(t, http.MethodGet, "/api/user/me", nil)
	require.Equal(t, http.StatusOK, status)
	var user db.User
	require.NoError(t, json.Unmarshal(data, &user))
	require.NotZero(t, user.ID)
	t.Cleanup(func() {
		status, _ := hub.request(t, http.MethodDelete, fmt.Sprintf("/api/admin/users/%d", user.ID), nil)
		assert.Equal(t, http.StatusOK, status)
	})
	return reader, user.ID
}

func TestFleet(t *testing.T) {
	require.Equal(t, "1", os.Getenv("LIGHTHOUSE_INTEGRATION"), "start tests/integration/compose.yaml and explicitly enable the isolated integration suite")
	hub := authenticatedFixture(t, "http://hub:8000")
	standalone := authenticatedFixture(t, "http://standalone:8000")
	workloads := []fixtureWorkload{
		{engine: "engine-hub", node: "hub-test", name: "verify-hub-web"},
		{engine: "engine-one", node: "spoke-one-test", name: "verify-one-web"},
		{engine: "engine-two", node: "spoke-two-test", name: "verify-two-database"},
	}
	for index := range workloads {
		workload := &workloads[index]
		t.Logf("Preparing workload on %s", workload.node)
		engineCommand(t, workload.engine, "pull", "alpine:3.24")
		existing := engineCommand(t, workload.engine, "ps", "--all", "--quiet", "--filter", "name=^/"+workload.name+"$", "--filter", "label=lighthouse.test=integration")
		if existing != "" {
			engineCommand(t, workload.engine, append([]string{"rm", "--force"}, strings.Fields(existing)...)...)
		}
		arguments := []string{"run", "--detach", "--name", workload.name, "--label", "lighthouse.test=integration", "--memory", "64m", "--env", "VERIFY_SECRET=must-not-leak", "--env", "PUBLIC_VALUE=visible"}
		if index == 1 {
			arguments = append(arguments, "--tty")
		}
		arguments = append(arguments, "alpine:3.24", "sh", "-c", "printf 'integration stdout\\nintegration stderr\\n'; trap 'exit 0' TERM INT; while :; do sleep 1; done")
		workload.id = engineCommand(t, workload.engine, arguments...)
	}

	t.Run("inventory_and_node_ownership", func(t *testing.T) {
		require.Eventually(t, func() bool {
			items := hub.containers(t)
			if len(items) != len(workloads) {
				return false
			}
			for _, workload := range workloads {
				found := false
				for _, item := range items {
					if item.ID == workload.id[:12] && item.NodeID == workload.node && item.Name == workload.name {
						found = true
					}
				}
				if !found {
					return false
				}
			}
			return true
		}, 90*time.Second, time.Second)
		items := hub.containers(t)
		for _, workload := range workloads {
			var found *fixtureContainer
			for index := range items {
				if items[index].Name == workload.name {
					found = &items[index]
				}
			}
			require.NotNil(t, found)
			assert.Equal(t, workload.id[:12], found.ID)
			assert.Equal(t, workload.node, found.NodeID)
			assert.Equal(t, workload.node != "hub-test", found.IsRemote)
			for _, capability := range []string{"inspect", "logs", "shell", "actions", "scan"} {
				assert.True(t, found.Capabilities[capability], "%s on %s", capability, workload.node)
			}
		}
		localItems := standalone.containers(t)
		require.Len(t, localItems, 1)
		assert.Equal(t, "standalone-test", localItems[0].NodeID)
		assert.False(t, localItems[0].IsRemote)
	})

	t.Run("unauthenticated_requests_are_denied", func(t *testing.T) {
		anonymous := &fixtureAPI{baseURL: hub.baseURL, client: hub.client}
		for _, path := range []string{"/api/containers", "/api/admin/nodes", "/api/admin/users", "/api/tokens"} {
			status, _ := anonymous.request(t, http.MethodGet, path, nil)
			assert.Equal(t, http.StatusUnauthorized, status, path)
		}
	})

	t.Run("inspection_redacts_environment_secrets", func(t *testing.T) {
		for _, workload := range workloads {
			status, data := hub.request(t, http.MethodGet, "/api/containers/"+workload.id+"/inspect", nil)
			require.Equal(t, http.StatusOK, status, workload.node)
			assert.False(t, strings.Contains(string(data), "must-not-leak"), "inspect leaked the fixture secret on %s", workload.node)
			assert.Contains(t, string(data), "visible")
			assert.Contains(t, string(data), workload.name)
		}
	})

	t.Run("local_and_remote_log_streams", func(t *testing.T) {
		for _, workload := range workloads {
			t.Run(workload.node, func(t *testing.T) {
				connection := hub.socket(t, "/ws/logs/"+workload.id)
				var output strings.Builder
				for !strings.Contains(output.String(), "integration stderr") {
					_, message, err := connection.ReadMessage()
					require.NoError(t, err)
					output.Write(message)
				}
				assert.Contains(t, output.String(), "integration stdout")
			})
		}
	})

	t.Run("local_and_remote_interactive_shell", func(t *testing.T) {
		for _, workload := range workloads {
			t.Run(workload.node, func(t *testing.T) {
				connection := hub.socket(t, "/ws/shell/"+workload.id+"?shell=/bin/sh")
				marker := "__verified_shell_" + workload.node + "__"
				require.NoError(t, connection.WriteMessage(websocket.TextMessage, []byte("printf '\\n"+marker+"\\n'; exit\n")))
				var output strings.Builder
				for !strings.Contains(output.String(), "\r\n"+marker+"\r\n") {
					_, message, err := connection.ReadMessage()
					require.NoError(t, err, "shell output: %s", output.String())
					output.Write(message)
				}
			})
		}
	})

	t.Run("actions_reach_only_the_owning_engine", func(t *testing.T) {
		for _, workload := range workloads {
			for _, action := range []string{"stop", "start", "restart"} {
				status, _ := hub.request(t, http.MethodPost, "/api/containers/"+workload.id+"/action", url.Values{"action": {action}})
				require.Equal(t, http.StatusOK, status, "%s on %s", action, workload.node)
				running := engineCommand(t, workload.engine, "inspect", "--format", "{{.State.Running}}", workload.id)
				assert.Equal(t, fmt.Sprint(action != "stop"), running)
			}
		}
	})

	t.Run("restricted_user_and_live_permission_changes", func(t *testing.T) {
		reader, userID := restrictedFixture(t, hub)
		items := reader.containers(t)
		require.Len(t, items, 1)
		assert.Equal(t, "spoke-one-test", items[0].NodeID)
		status, _ := reader.request(t, http.MethodGet, "/api/containers/"+workloads[1].id+"/inspect", nil)
		assert.Equal(t, http.StatusOK, status)
		for _, workload := range []fixtureWorkload{workloads[0], workloads[2]} {
			status, _ := reader.request(t, http.MethodGet, "/api/containers/"+workload.id+"/inspect", nil)
			assert.Equal(t, http.StatusForbidden, status, workload.node)
			header := http.Header{"Authorization": {"Bearer " + reader.token}, "Origin": {reader.baseURL}}
			connection, response, err := websocket.DefaultDialer.Dial(strings.Replace(reader.baseURL, "http://", "ws://", 1)+"/ws/logs/"+workload.id, header)
			if connection != nil {
				_ = connection.Close()
			}
			require.Error(t, err)
			require.NotNil(t, response)
			assert.Equal(t, http.StatusForbidden, response.StatusCode)
			_ = response.Body.Close()
		}
		for _, path := range []string{"/api/admin/nodes", "/api/admin/users", "/api/admin/settings", "/api/system/history?node_id=spoke-two-test"} {
			status, _ := reader.request(t, http.MethodGet, path, nil)
			assert.Equal(t, http.StatusForbidden, status, path)
		}
		for _, action := range []string{"start", "stop", "restart", "remove"} {
			status, _ := reader.request(t, http.MethodPost, "/api/containers/"+workloads[1].id+"/action", url.Values{"action": {action}})
			assert.Equal(t, http.StatusForbidden, status, action)
		}
		status, _ = reader.request(t, http.MethodDelete, "/api/volumes/forbidden?node_id=spoke-one-test", nil)
		assert.Equal(t, http.StatusForbidden, status)
		status, _ = hub.request(t, http.MethodPut, fmt.Sprintf("/api/admin/users/%d/permissions", userID), url.Values{
			"is_restricted_access": {"true"}, "allowed_containers": {"^verify-two-database$"},
		})
		require.Equal(t, http.StatusOK, status)
		items = reader.containers(t)
		require.Len(t, items, 1)
		assert.Equal(t, "spoke-two-test", items[0].NodeID, "permission changes must apply without logging in again")
		status, _ = reader.request(t, http.MethodGet, "/api/containers/"+workloads[1].id+"/inspect", nil)
		assert.Equal(t, http.StatusForbidden, status, "revoked container access remained valid")
	})

	t.Run("api_tokens_are_hashed_scoped_and_revocable", func(t *testing.T) {
		status, data := hub.request(t, http.MethodPost, "/api/tokens", map[string]string{"name": "integration-token"})
		require.Equal(t, http.StatusOK, status)
		var issued struct {
			ID    uint   `json:"id"`
			Token string `json:"token"`
		}
		require.NoError(t, json.Unmarshal(data, &issued))
		require.NotEmpty(t, issued.Token)
		connection := fixtureDatabase(t)
		var stored db.ApiToken
		require.NoError(t, connection.First(&stored, issued.ID).Error)
		digest := sha256.Sum256([]byte(issued.Token))
		assert.Equal(t, hex.EncodeToString(digest[:]), stored.Token)
		pat := &fixtureAPI{baseURL: hub.baseURL, client: hub.client, token: issued.Token}
		status, _ = pat.request(t, http.MethodGet, "/api/containers", nil)
		require.Equal(t, http.StatusOK, status)
		status, data = hub.request(t, http.MethodGet, "/api/tokens", nil)
		require.Equal(t, http.StatusOK, status)
		assert.False(t, strings.Contains(string(data), issued.Token), "token list leaked a plaintext credential")
		status, _ = hub.request(t, http.MethodDelete, fmt.Sprintf("/api/tokens/%d", issued.ID), nil)
		require.Equal(t, http.StatusOK, status)
		status, _ = pat.request(t, http.MethodGet, "/api/containers", nil)
		assert.Equal(t, http.StatusUnauthorized, status)
	})

	t.Run("resource_ownership_delete_and_prune", func(t *testing.T) {
		resourceName := fmt.Sprintf("verify-shared-%d", time.Now().UnixNano())
		networkIDs := make(map[string]string)
		for _, workload := range workloads {
			engineCommand(t, workload.engine, "volume", "create", "--label", "lighthouse.test=integration", resourceName)
			networkIDs[workload.node] = engineCommand(t, workload.engine, "network", "create", "--label", "lighthouse.test=integration", resourceName)
		}
		for _, endpoint := range []string{"volumes", "networks", "images"} {
			status, data := hub.request(t, http.MethodGet, "/api/"+endpoint, nil)
			require.Equal(t, http.StatusOK, status)
			var resources []map[string]interface{}
			require.NoError(t, json.Unmarshal(data, &resources))
			for _, workload := range workloads {
				found := false
				for _, resource := range resources {
					if resource["node_id"] == workload.node && (endpoint == "images" || resource["Name"] == resourceName) {
						found = true
					}
				}
				assert.True(t, found, "%s inventory omitted %s", endpoint, workload.node)
			}
		}
		status, _ := hub.request(t, http.MethodDelete, "/api/volumes/"+resourceName+"?node_id=spoke-two-test", nil)
		require.Equal(t, http.StatusOK, status)
		status, _ = hub.request(t, http.MethodDelete, "/api/networks/"+networkIDs["spoke-two-test"]+"?node_id=spoke-two-test", nil)
		require.Equal(t, http.StatusOK, status)
		for _, workload := range workloads[:2] {
			assert.Contains(t, engineCommand(t, workload.engine, "volume", "inspect", resourceName), resourceName)
			assert.Contains(t, engineCommand(t, workload.engine, "network", "inspect", networkIDs[workload.node]), resourceName)
		}
		assert.Empty(t, engineCommand(t, "engine-two", "volume", "ls", "--quiet", "--filter", "name="+resourceName))
		assert.Empty(t, engineCommand(t, "engine-two", "network", "ls", "--quiet", "--filter", "name="+resourceName))
		for _, endpoint := range []string{"images", "volumes", "networks"} {
			status, data := hub.request(t, http.MethodPost, "/api/"+endpoint+"/prune?node_id=spoke-two-test", map[string]bool{"remove_containers": false})
			require.Equal(t, http.StatusOK, status)
			var response map[string]interface{}
			require.NoError(t, json.Unmarshal(data, &response))
			assert.Contains(t, response, "Report")
		}
		for _, workload := range workloads {
			assert.Equal(t, "true", engineCommand(t, workload.engine, "inspect", "--format", "{{.State.Running}}", workload.id))
		}
	})

	t.Run("postgres_metric_values_reach_history", func(t *testing.T) {
		connection := fixtureDatabase(t)
		for _, workload := range workloads {
			var saved db.SystemStat
			require.Eventually(t, func() bool {
				return connection.Where("node_id = ?", workload.node).Order("timestamp DESC").First(&saved).Error == nil
			}, 90*time.Second, time.Second)
			status, data := hub.request(t, http.MethodGet, "/api/system/history?node_id="+url.QueryEscape(workload.node)+"&duration=1h", nil)
			require.Equal(t, http.StatusOK, status)
			var history []map[string]interface{}
			require.NoError(t, json.Unmarshal(data, &history))
			require.NotEmpty(t, history)
			found := false
			for _, row := range history {
				assert.Equal(t, workload.node, row["node_id"])
				timestamp, err := time.Parse(time.RFC3339Nano, row["timestamp"].(string))
				require.NoError(t, err)
				if timestamp.Equal(saved.Timestamp) {
					found = true
					assert.Equal(t, saved.CPU, row["cpu"])
					assert.Equal(t, float64(saved.Memory), row["memory"])
					assert.Equal(t, float64(saved.NetRxBytes), row["net_rx"])
					assert.Equal(t, float64(saved.NetTxBytes), row["net_tx"])
					assert.Equal(t, float64(saved.DiskReadBytes), row["disk_read"])
					assert.Equal(t, float64(saved.DiskWriteBytes), row["disk_write"])
				}
			}
			assert.True(t, found, "saved sample missing from API history on %s", workload.node)
			var containerSamples int64
			require.Eventually(t, func() bool {
				return connection.Model(&db.Stat{}).Where("node_id = ? AND container_id = ?", workload.node, workload.id).Count(&containerSamples).Error == nil && containerSamples > 0
			}, 90*time.Second, time.Second, "no container samples arrived from %s", workload.node)
		}
	})
}

func TestDisconnectedFleet(t *testing.T) {
	require.Equal(t, "disconnected", os.Getenv("LIGHTHOUSE_FAULT_PHASE"), "this phase requires spoke-one to be stopped by the integration runner")
	hub := authenticatedFixture(t, "http://hub:8000")
	require.Eventually(t, func() bool {
		status, data := hub.request(t, http.MethodGet, "/api/admin/nodes", nil)
		if status != http.StatusOK {
			return false
		}
		var nodes []struct {
			ID        string `json:"id"`
			Connected bool   `json:"connected"`
		}
		if json.Unmarshal(data, &nodes) != nil {
			return false
		}
		firstOffline, secondOnline := false, false
		for _, node := range nodes {
			firstOffline = firstOffline || (node.ID == "spoke-one-test" && !node.Connected)
			secondOnline = secondOnline || (node.ID == "spoke-two-test" && node.Connected)
		}
		return firstOffline && secondOnline
	}, 30*time.Second, time.Second)
	items := hub.containers(t)
	require.Len(t, items, 2)
	for _, item := range items {
		assert.NotEqual(t, "spoke-one-test", item.NodeID)
		status, _ := hub.request(t, http.MethodGet, "/api/containers/"+item.ID+"/inspect", nil)
		assert.Equal(t, http.StatusOK, status)
	}
	disconnectedID := engineCommand(t, "engine-one", "inspect", "--format", "{{.Id}}", "verify-one-web")
	status, _ := hub.request(t, http.MethodPost, "/api/containers/"+disconnectedID+"/action", url.Values{"action": {"stop"}})
	assert.NotEqual(t, http.StatusOK, status, "disconnected node action was reported as successful")
	assert.Equal(t, "true", engineCommand(t, "engine-one", "inspect", "--format", "{{.State.Running}}", disconnectedID))
}

func TestRecoveredFleet(t *testing.T) {
	require.Equal(t, "recovered", os.Getenv("LIGHTHOUSE_FAULT_PHASE"), "this phase requires the runner to restart the hub and stopped spoke")
	hub := authenticatedFixture(t, "http://hub:8000")
	require.Eventually(t, func() bool {
		items := hub.containers(t)
		seen := make(map[string]bool)
		for _, item := range items {
			seen[item.NodeID] = true
		}
		return seen["hub-test"] && seen["spoke-one-test"] && seen["spoke-two-test"]
	}, 90*time.Second, time.Second)
	for _, item := range hub.containers(t) {
		status, _ := hub.request(t, http.MethodGet, "/api/containers/"+item.ID+"/inspect", nil)
		assert.Equal(t, http.StatusOK, status)
		status, data := hub.request(t, http.MethodGet, "/api/system/history?node_id="+url.QueryEscape(item.NodeID)+"&duration=1h", nil)
		require.Equal(t, http.StatusOK, status)
		var history []map[string]interface{}
		require.NoError(t, json.Unmarshal(data, &history))
		assert.NotEmpty(t, history, "PostgreSQL history was lost on restart")
	}
}

func TestScanFleet(t *testing.T) {
	require.Equal(t, "1", os.Getenv("LIGHTHOUSE_INTEGRATION"), "the isolated integration fleet is required")
	hub := authenticatedFixture(t, "http://hub:8000")
	standalone := authenticatedFixture(t, "http://standalone:8000")
	tests := []struct {
		name      string
		api       *fixtureAPI
		engine    string
		container string
	}{
		{name: "remote scan and cache reuse", api: hub, engine: "engine-one", container: "verify-one-web"},
		{name: "standalone scan and cache reuse", api: standalone, engine: "engine-hub", container: "verify-hub-web"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			containerID := engineCommand(t, test.engine, "inspect", "--format", "{{.Id}}", test.container)
			var previousID int
			status, data := test.api.request(t, http.MethodGet, "/api/images/scans?image=alpine:3.24", nil)
			if status == http.StatusOK {
				var previous db.ImageScanResult
				require.NoError(t, json.Unmarshal(data, &previous))
				previousID = previous.ID
			} else {
				require.Equal(t, http.StatusNoContent, status)
			}
			for attempt := 0; attempt < 2; attempt++ {
				t.Logf("Starting scan attempt %d", attempt+1)
				status, _ = test.api.request(t, http.MethodPost, "/api/containers/"+containerID+"/scan", nil)
				require.Equal(t, http.StatusOK, status)
				var result db.ImageScanResult
				polls := 0
				require.Eventually(t, func() bool {
					polls++
					if polls%15 == 0 {
						t.Logf("Waiting for scan attempt %d (%d seconds)", attempt+1, polls*2)
					}
					status, data := test.api.request(t, http.MethodGet, "/api/images/scans?image=alpine:3.24", nil)
					return status == http.StatusOK && json.Unmarshal(data, &result) == nil && result.ID > previousID
				}, 6*time.Minute, 2*time.Second)
				var report map[string]interface{}
				require.NoError(t, json.Unmarshal([]byte(result.Result), &report))
				require.Empty(t, report["Error"], "real scan failed: %v", report["Error"])
				assert.Contains(t, report, "Results")
				previousID = result.ID
			}
			assert.Contains(t, engineCommand(t, test.engine, "volume", "ls", "--quiet", "--filter", "name=trivy-vuln-db"), "trivy-vuln-db")
		})
	}
	t.Run("failed standalone scan is visible", func(t *testing.T) {
		containerID := engineCommand(t, "engine-hub", "inspect", "--format", "{{.Id}}", "verify-hub-web")
		image := "127.0.0.1:1/verification-missing:latest"
		var previous db.ImageScanResult
		status, data := standalone.request(t, http.MethodGet, "/api/images/scans?image="+url.QueryEscape(image), nil)
		if status == http.StatusOK {
			require.NoError(t, json.Unmarshal(data, &previous))
		}
		status, _ = standalone.request(t, http.MethodPost, "/api/containers/"+containerID+"/scan?image="+url.QueryEscape(image), nil)
		require.Equal(t, http.StatusOK, status)
		var result db.ImageScanResult
		require.Eventually(t, func() bool {
			status, data := standalone.request(t, http.MethodGet, "/api/images/scans?image="+url.QueryEscape(image), nil)
			return status == http.StatusOK && json.Unmarshal(data, &result) == nil && result.ID > previous.ID
		}, 6*time.Minute, 2*time.Second)
		var report map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(result.Result), &report))
		assert.NotEmpty(t, report["Error"])
	})
}
