package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"

	"github.com/stretchr/testify/assert"
)

func TestRegisterVolumeRoutes(t *testing.T) {
	// GET /volumes success
	t.Run("GetVolumes_Success", func(t *testing.T) {
		handler := func(req *http.Request) (*http.Response, error) {
			if strings.HasSuffix(req.URL.Path, "/volumes") && req.Method == http.MethodGet {
				volList := volume.ListResponse{
					Volumes: []volume.Volume{
						{Name: "vol1"},
					},
				}
				b, _ := json.Marshal(volList)
				return makeResponse(200, string(b)), nil
			}
			return nil, errors.New("unexpected request")
		}
		cli := mockDockerClientWithRoundTripper(t, handler)
		_, _, c, rec := setupEchoWithClaimsHelper(nil)

		c.Request().Method = http.MethodGet
		c.Request().URL.Path = "/volumes"
		err := handleGETVolumes(cli)(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "vol1")
	})

	// GET /volumes error
	t.Run("GetVolumes_Error", func(t *testing.T) {
		handler := func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("docker error")
		}
		cli := mockDockerClientWithRoundTripper(t, handler)
		_, _, c, rec := setupEchoWithClaimsHelper(nil)

		c.Request().Method = http.MethodGet
		c.Request().URL.Path = "/volumes"
		err := handleGETVolumes(cli)(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "docker error")
	})

	// DELETE /volumes/:name forbidden
	t.Run("DeleteVolume_Forbidden", func(t *testing.T) {
		cli := mockDockerClientWithRoundTripper(t, nil)
		claims := &UserClaims{IsAdmin: false, CanDelete: false}
		_, _, c, rec := setupEchoWithClaimsHelper(claims)

		c.Request().Method = http.MethodDelete
		c.Request().URL.Path = "/volumes/vol1"
		c.SetParamNames("name")
		c.SetParamValues("vol1")
		err := handleDELETEVolumesName(cli)(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	// DELETE /volumes/:name success
	t.Run("DeleteVolume_Success", func(t *testing.T) {
		handler := func(req *http.Request) (*http.Response, error) {
			if strings.HasSuffix(req.URL.Path, "/volumes/vol1") && req.Method == http.MethodDelete {
				return makeResponse(204, ""), nil
			}
			return nil, errors.New("unexpected request")
		}
		cli := mockDockerClientWithRoundTripper(t, handler)
		claims := &UserClaims{ID: 1, Username: "admin", IsAdmin: true, CanDelete: true}
		_, _, c, rec := setupEchoWithClaimsHelper(claims)

		c.Request().Method = http.MethodDelete
		c.Request().URL.Path = "/volumes/vol1"
		c.SetParamNames("name")
		c.SetParamValues("vol1")
		err := handleDELETEVolumesName(cli)(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	// DELETE /volumes/:name error
	t.Run("DeleteVolume_Error", func(t *testing.T) {
		handler := func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("docker delete error")
		}
		cli := mockDockerClientWithRoundTripper(t, handler)
		claims := &UserClaims{ID: 1, Username: "admin", IsAdmin: true, CanDelete: true}
		_, _, c, rec := setupEchoWithClaimsHelper(claims)

		c.Request().Method = http.MethodDelete
		c.Request().URL.Path = "/volumes/vol1"
		c.SetParamNames("name")
		c.SetParamValues("vol1")
		err := handleDELETEVolumesName(cli)(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "docker delete error")
	})

	// POST /volumes/prune forbidden
	t.Run("PruneVolumes_Forbidden", func(t *testing.T) {
		cli := mockDockerClientWithRoundTripper(t, nil)
		claims := &UserClaims{IsAdmin: false, CanDelete: false}
		_, _, c, rec := setupEchoWithClaimsHelper(claims)

		c.Request().Method = http.MethodPost
		c.Request().URL.Path = "/volumes/prune"
		err := handlePOSTVolumesPrune(cli)(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	// POST /volumes/prune with RemoveContainers true
	t.Run("PruneVolumes_RemoveContainers", func(t *testing.T) {
		handler := func(req *http.Request) (*http.Response, error) {
			if strings.HasSuffix(req.URL.Path, "/containers/prune") && req.Method == http.MethodPost {
				return makeResponse(200, "{}"), nil
			}
			if strings.HasSuffix(req.URL.Path, "/volumes/prune") && req.Method == http.MethodPost {
				return makeResponse(200, `{"VolumesDeleted": ["vol1"]}`), nil
			}
			return nil, errors.New("unexpected request " + req.URL.Path)
		}
		cli := mockDockerClientWithRoundTripper(t, handler)
		claims := &UserClaims{ID: 1, Username: "admin", IsAdmin: true, CanDelete: true}
		_, _, c, rec := setupEchoWithClaimsHelper(claims)

		reqBody := `{"remove_containers": true}`
		req := httptest.NewRequest(http.MethodPost, "/volumes/prune", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		c.SetRequest(req)

		err := handlePOSTVolumesPrune(cli)(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "vol1")
	})

	// POST /volumes/prune with RemoveContainers false, no stopped containers
	t.Run("PruneVolumes_NoRemoveContainers_NoStopped", func(t *testing.T) {
		handler := func(req *http.Request) (*http.Response, error) {
			if strings.HasSuffix(req.URL.Path, "/containers/json") && req.Method == http.MethodGet {
				return makeResponse(200, `[]`), nil
			}
			if strings.HasSuffix(req.URL.Path, "/volumes/prune") && req.Method == http.MethodPost {
				return makeResponse(200, `{"VolumesDeleted": ["vol2"]}`), nil
			}
			return nil, errors.New("unexpected request " + req.URL.Path)
		}
		cli := mockDockerClientWithRoundTripper(t, handler)
		claims := &UserClaims{ID: 1, Username: "admin", IsAdmin: true, CanDelete: true}
		_, _, c, rec := setupEchoWithClaimsHelper(claims)

		reqBody := `{"remove_containers": false}`
		req := httptest.NewRequest(http.MethodPost, "/volumes/prune", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		c.SetRequest(req)

		err := handlePOSTVolumesPrune(cli)(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "vol2")
		assert.NotContains(t, rec.Body.String(), "Stopped containers detected")
	})

	// POST /volumes/prune with RemoveContainers false, with stopped containers
	t.Run("PruneVolumes_NoRemoveContainers_WithStopped", func(t *testing.T) {
		handler := func(req *http.Request) (*http.Response, error) {
			if strings.HasSuffix(req.URL.Path, "/containers/json") && req.Method == http.MethodGet {
				return makeResponse(200, `[{"Id": "c1"}]`), nil
			}
			if strings.HasSuffix(req.URL.Path, "/volumes/prune") && req.Method == http.MethodPost {
				return makeResponse(200, `{"VolumesDeleted": ["vol3"]}`), nil
			}
			return nil, errors.New("unexpected request " + req.URL.Path)
		}
		cli := mockDockerClientWithRoundTripper(t, handler)
		claims := &UserClaims{ID: 1, Username: "admin", IsAdmin: true, CanDelete: true}
		_, _, c, rec := setupEchoWithClaimsHelper(claims)

		reqBody := `{"remove_containers": false}`
		req := httptest.NewRequest(http.MethodPost, "/volumes/prune", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		c.SetRequest(req)

		err := handlePOSTVolumesPrune(cli)(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "Stopped containers detected")
	})

	// POST /volumes/prune error
	t.Run("PruneVolumes_Error", func(t *testing.T) {
		handler := func(req *http.Request) (*http.Response, error) {
			if strings.HasSuffix(req.URL.Path, "/containers/json") && req.Method == http.MethodGet {
				return makeResponse(200, `[]`), nil
			}
			if strings.HasSuffix(req.URL.Path, "/volumes/prune") && req.Method == http.MethodPost {
				return nil, errors.New("docker prune error")
			}
			return nil, errors.New("unexpected request " + req.URL.Path)
		}
		cli := mockDockerClientWithRoundTripper(t, handler)
		claims := &UserClaims{ID: 1, Username: "admin", IsAdmin: true, CanDelete: true}
		_, _, c, rec := setupEchoWithClaimsHelper(claims)

		req := httptest.NewRequest(http.MethodPost, "/volumes/prune", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		c.SetRequest(req)

		err := handlePOSTVolumesPrune(cli)(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "docker prune error")
	})
}

func TestRegisterVolumeRoutes_Coverage(t *testing.T) {
	e := echo.New()
	g := e.Group("/api")
	cli, _ := client.NewClientWithOpts(client.WithVersion("1.41"))
	RegisterVolumeRoutes(g, cli)
	assert.NotNil(t, g)
}
