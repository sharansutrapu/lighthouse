package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"lighthouse/db"
)

// ─────────────────────────────────────────────────────────────────────────
// cpuTracker: concurrency-focused tests for the mutex-protected replacement
// of the old bare `map[string][2]uint64` shared across statPollLoop's
// worker-pool goroutines.
// ─────────────────────────────────────────────────────────────────────────

func TestCPUTracker_GetSetDelete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(t *testing.T, tr *cpuTracker)
	}{
		{
			name: "happy path: set then get returns the stored value",
			run: func(t *testing.T, tr *cpuTracker) {
				tr.set("c1", [2]uint64{100, 200})
				v, ok := tr.get("c1")
				assert.True(t, ok)
				assert.Equal(t, [2]uint64{100, 200}, v)
			},
		},
		{
			name: "hostile: get on a key that was never set",
			run: func(t *testing.T, tr *cpuTracker) {
				_, ok := tr.get("never-set")
				assert.False(t, ok)
			},
		},
		{
			name: "hostile: get on empty string key",
			run: func(t *testing.T, tr *cpuTracker) {
				_, ok := tr.get("")
				assert.False(t, ok)
			},
		},
		{
			name: "infra failure: delete after container disappears prevents stale CPU deltas",
			run: func(t *testing.T, tr *cpuTracker) {
				tr.set("c2", [2]uint64{5, 10})
				tr.delete("c2")
				_, ok := tr.get("c2")
				assert.False(t, ok)
			},
		},
		{
			name: "delete of a never-set key is a no-op, not a panic",
			run: func(t *testing.T, tr *cpuTracker) {
				assert.NotPanics(t, func() { tr.delete("does-not-exist") })
			},
		},
		{
			name: "overwrite: second set replaces the first value",
			run: func(t *testing.T, tr *cpuTracker) {
				tr.set("c3", [2]uint64{1, 1})
				tr.set("c3", [2]uint64{9, 9})
				v, ok := tr.get("c3")
				assert.True(t, ok)
				assert.Equal(t, [2]uint64{9, 9}, v)
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.run(t, newCPUTracker())
		})
	}
}

// TestCPUTracker_ConcurrentAccess hammers a single shared *cpuTracker from
// many goroutines simultaneously performing get/set/delete. Run with
// `go test -race` to prove the internal mutex actually serializes access —
// before this fix, statPollLoop shared a bare map across worker-pool
// goroutines, which is a data race the moment more than one container is
// polled concurrently.
func TestCPUTracker_ConcurrentAccess(t *testing.T) {
	tr := newCPUTracker()
	const goroutines = 50
	const opsPerGoroutine = 200

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			id := fmt.Sprintf("container-%d", g%10) // force overlapping keys across goroutines
			for i := 0; i < opsPerGoroutine; i++ {
				tr.set(id, [2]uint64{uint64(i), uint64(i * 2)})
				tr.get(id)
				if i%17 == 0 {
					tr.delete(id)
				}
			}
		}(g)
	}
	wg.Wait()
	// Reaching here without the race detector firing (and without a panic)
	// is the assertion: the mutex correctly serializes concurrent access.
}

// ─────────────────────────────────────────────────────────────────────────
// statPollLoop / pollOneStat: the polling engine itself.
// ─────────────────────────────────────────────────────────────────────────

// TestStatPollInFlightGuard directly exercises the atomic CAS guard that
// makes statPollLoop skip a tick instead of queueing it up behind a
// still-running previous tick (the fix for unbounded pile-up under load).
func TestStatPollInFlightGuard(t *testing.T) {
	atomic.StoreInt32(&statPollInFlight, 0)
	defer atomic.StoreInt32(&statPollInFlight, 0)

	if !atomic.CompareAndSwapInt32(&statPollInFlight, 0, 1) {
		t.Fatal("first CAS(0->1) should succeed when no poll is in flight")
	}
	if atomic.CompareAndSwapInt32(&statPollInFlight, 0, 1) {
		t.Fatal("second CAS(0->1) should fail while a poll is already in flight — this is the overlap-prevention guard")
	}
	atomic.StoreInt32(&statPollInFlight, 0)
	if !atomic.CompareAndSwapInt32(&statPollInFlight, 0, 1) {
		t.Fatal("CAS(0->1) should succeed again once the in-flight flag is reset to 0")
	}
}

// TestStatPollLoop_BoundedConcurrency proves the worker pool actually
// parallelizes per-container stat fetches (not a plain sequential loop,
// which cannot keep up with the 2s cadence once a fleet grows into the
// hundreds/thousands) while still respecting the statPollMaxConcurrency cap.
func TestStatPollLoop_BoundedConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("real-time ticker test; skipped in -short mode")
	}
	atomic.StoreInt32(&statPollInFlight, 0)

	const numContainers = 50
	var containersJSON strings.Builder
	containersJSON.WriteByte('[')
	for i := 0; i < numContainers; i++ {
		if i > 0 {
			containersJSON.WriteByte(',')
		}
		fmt.Fprintf(&containersJSON, `{"Id":"bc-container-%d","State":"running"}`, i)
	}
	containersJSON.WriteByte(']')

	var current int32
	var maxObserved int32
	cli := mockDockerClientWithRoundTripper(t, func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/containers/json") {
			return makeResponse(http.StatusOK, containersJSON.String()), nil
		}
		n := atomic.AddInt32(&current, 1)
		for {
			old := atomic.LoadInt32(&maxObserved)
			if n <= old || atomic.CompareAndSwapInt32(&maxObserved, old, n) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond) // widen the concurrency window so overlap is observable
		atomic.AddInt32(&current, -1)
		return makeResponse(http.StatusOK, `{"cpu_stats":{},"memory_stats":{},"networks":{},"blkio_stats":{}}`), nil
	})

	go statPollLoop(cli)
	time.Sleep(2300 * time.Millisecond) // allow one full 2s tick to run to completion

	observed := atomic.LoadInt32(&maxObserved)
	if observed <= 1 {
		t.Fatalf("max observed concurrent stat fetches = %d, want > 1 (statPollLoop must not be strictly sequential)", observed)
	}
	if observed > int32(statPollMaxConcurrency) {
		t.Fatalf("max observed concurrent stat fetches = %d, want <= %d (worker pool must be bounded)", observed, statPollMaxConcurrency)
	}
}

// TestStatPollLoop_ContainerListErrorIsLogged verifies the (previously
// silent) ContainerList failure path now records lastPollErrorUnix instead
// of just `continue`-ing without a trace — the fix for the "false healthy
// dashboard during a Docker daemon outage" failure mode.
func TestStatPollLoop_ContainerListErrorIsLogged(t *testing.T) {
	if testing.Short() {
		t.Skip("real-time ticker test; skipped in -short mode")
	}
	atomic.StoreInt32(&statPollInFlight, 0)
	atomic.StoreInt64(&lastPollErrorUnix, 0)

	before := time.Now().Unix()
	cli := mockDockerClientWithRoundTripper(t, func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("simulated docker daemon unreachable")
	})

	go statPollLoop(cli)
	time.Sleep(2300 * time.Millisecond)

	errTs := atomic.LoadInt64(&lastPollErrorUnix)
	if errTs < before {
		t.Fatalf("lastPollErrorUnix = %d, want it updated to a timestamp >= %d after a ContainerList failure", errTs, before)
	}
}

// TestPollOneStat_Table covers pollOneStat's happy, hostile and
// infra-failure branches directly, with a fully mocked Docker client.
func TestPollOneStat_Table(t *testing.T) {
	tests := []struct {
		name          string
		containerID   string
		handler       func(req *http.Request) (*http.Response, error)
		wantCacheHit  bool
		wantCPU       float64
		wantErrLogged bool // informational only; pollOneStat has no return value to assert on
	}{
		{
			name:        "happy path: valid stats decode and populate the cache",
			containerID: "poll-happy",
			handler: func(req *http.Request) (*http.Response, error) {
				return makeResponse(http.StatusOK, `{"cpu_stats":{"cpu_usage":{"total_usage":1000},"system_cpu_usage":2000,"online_cpus":2},"memory_stats":{"usage":500},"networks":{},"blkio_stats":{"io_service_bytes_recursive":[]}}`), nil
			},
			wantCacheHit: true,
		},
		{
			name:        "infra failure: ContainerStats round trip returns an error",
			containerID: "poll-infra-fail",
			handler: func(req *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("simulated connection reset")
			},
			wantCacheHit: false,
		},
		{
			name:        "hostile: malformed / truncated JSON body",
			containerID: "poll-bad-json",
			handler: func(req *http.Request) (*http.Response, error) {
				return makeResponse(http.StatusOK, `{"cpu_stats": not-json`), nil
			},
			wantCacheHit: false,
		},
		{
			name:        "infra failure: non-200 status with empty body",
			containerID: "poll-500",
			handler: func(req *http.Request) (*http.Response, error) {
				return makeResponse(http.StatusInternalServerError, ``), nil
			},
			wantCacheHit: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			liveStatsMu.Lock()
			delete(liveStatsCache, tc.containerID)
			liveStatsMu.Unlock()

			cli := mockDockerClientWithRoundTripper(t, tc.handler)
			pollOneStat(cli, tc.containerID, newCPUTracker())

			liveStatsMu.RLock()
			_, ok := liveStatsCache[tc.containerID]
			liveStatsMu.RUnlock()

			assert.Equal(t, tc.wantCacheHit, ok, "liveStatsCache population mismatch for %q", tc.containerID)
		})
	}
}

// TestPollOneStat_CPUDeltaAndMemoryNetworkBlkioBranches exercises the
// branches that TestPollOneStat_Table's single-call cases can't reach:
// the CPU delta computed from a previous sample, each memory working-set
// subtraction fallback, and non-empty network/blkio accumulation.
func TestPollOneStat_CPUDeltaAndMemoryNetworkBlkioBranches(t *testing.T) {
	t.Run("happy path: second call with a prior sample computes a non-zero CPU delta", func(t *testing.T) {
		const id = "poll-cpu-delta"
		liveStatsMu.Lock()
		delete(liveStatsCache, id)
		liveStatsMu.Unlock()

		tr := newCPUTracker()
		tr.set(id, [2]uint64{1000, 2000})

		cli := mockDockerClientWithRoundTripper(t, func(req *http.Request) (*http.Response, error) {
			return makeResponse(http.StatusOK, `{"cpu_stats":{"cpu_usage":{"total_usage":3000},"system_cpu_usage":6000,"online_cpus":2}}`), nil
		})
		pollOneStat(cli, id, tr)

		liveStatsMu.RLock()
		entry, ok := liveStatsCache[id]
		liveStatsMu.RUnlock()
		assert.True(t, ok)
		assert.Greater(t, entry.CPU, 0.0)
	})

	t.Run("happy path: online_cpus=0 falls back to runtime.NumCPU()", func(t *testing.T) {
		const id = "poll-cpu-fallback"
		liveStatsMu.Lock()
		delete(liveStatsCache, id)
		liveStatsMu.Unlock()

		tr := newCPUTracker()
		tr.set(id, [2]uint64{1000, 2000})

		cli := mockDockerClientWithRoundTripper(t, func(req *http.Request) (*http.Response, error) {
			return makeResponse(http.StatusOK, `{"cpu_stats":{"cpu_usage":{"total_usage":3000},"system_cpu_usage":6000,"online_cpus":0}}`), nil
		})
		pollOneStat(cli, id, tr)

		liveStatsMu.RLock()
		_, ok := liveStatsCache[id]
		liveStatsMu.RUnlock()
		assert.True(t, ok)
	})

	for _, key := range []string{"inactive_file", "total_inactive_file", "cache"} {
		key := key
		t.Run("happy path: memory working-set subtracts "+key, func(t *testing.T) {
			id := "poll-mem-" + key
			liveStatsMu.Lock()
			delete(liveStatsCache, id)
			liveStatsMu.Unlock()

			body := fmt.Sprintf(`{"cpu_stats":{},"memory_stats":{"usage":1000,"stats":{"%s":200}}}`, key)
			cli := mockDockerClientWithRoundTripper(t, func(req *http.Request) (*http.Response, error) {
				return makeResponse(http.StatusOK, body), nil
			})
			pollOneStat(cli, id, newCPUTracker())

			liveStatsMu.RLock()
			entry, ok := liveStatsCache[id]
			liveStatsMu.RUnlock()
			assert.True(t, ok)
			assert.Equal(t, int64(800), entry.Memory)
		})
	}

	t.Run("happy path: network and blkio read/write bytes are accumulated", func(t *testing.T) {
		const id = "poll-net-blkio"
		liveStatsMu.Lock()
		delete(liveStatsCache, id)
		liveStatsMu.Unlock()

		body := `{"cpu_stats":{},"memory_stats":{"usage":0},"networks":{"eth0":{"rx_bytes":10,"tx_bytes":20},"eth1":{"rx_bytes":5,"tx_bytes":7}},"blkio_stats":{"io_service_bytes_recursive":[{"op":"Read","value":100},{"op":"Write","value":50},{"op":"read","value":25}]}}`
		cli := mockDockerClientWithRoundTripper(t, func(req *http.Request) (*http.Response, error) {
			return makeResponse(http.StatusOK, body), nil
		})
		pollOneStat(cli, id, newCPUTracker())

		liveStatsMu.RLock()
		entry, ok := liveStatsCache[id]
		liveStatsMu.RUnlock()
		assert.True(t, ok)
		assert.Equal(t, int64(15), entry.NetRxBytes)
		assert.Equal(t, int64(27), entry.NetTxBytes)
		assert.Equal(t, int64(125), entry.DiskReadBytes)
		assert.Equal(t, int64(50), entry.DiskWriteBytes)
	})
}

// ─────────────────────────────────────────────────────────────────────────
// collectStats: host + per-container metric snapshotting.
// ─────────────────────────────────────────────────────────────────────────

// TestCollectStats_ContainerDeltaAndSpokeModeBranches exercises the
// per-container prevStats-delta branch (second call sees growth from the
// first) and the "spoke" LighthouseMode branch that pushes to the hub
// instead of writing to the local DB.
func TestCollectStats_ContainerDeltaAndSpokeModeBranches(t *testing.T) {
	t.Run("happy path: second call computes a positive delta from the first", func(t *testing.T) {
		assert.NoError(t, db.InitDB(":memory:"))
		const id = "collect-delta-container"

		prevStatsMu.Lock()
		delete(prevStats, id)
		prevStatsMu.Unlock()
		liveStatsMu.Lock()
		liveStatsCache[id] = struct {
			CPU            float64
			Memory         int64
			NetRxBytes     int64
			NetTxBytes     int64
			DiskReadBytes  int64
			DiskWriteBytes int64
		}{CPU: 1.5, Memory: 100, NetRxBytes: 100, NetTxBytes: 100, DiskReadBytes: 100, DiskWriteBytes: 100}
		liveStatsMu.Unlock()
		t.Cleanup(func() {
			liveStatsMu.Lock()
			delete(liveStatsCache, id)
			liveStatsMu.Unlock()
			prevStatsMu.Lock()
			delete(prevStats, id)
			prevStatsMu.Unlock()
		})

		collectStats(getMockClient())

		liveStatsMu.Lock()
		liveStatsCache[id] = struct {
			CPU            float64
			Memory         int64
			NetRxBytes     int64
			NetTxBytes     int64
			DiskReadBytes  int64
			DiskWriteBytes int64
		}{CPU: 2.0, Memory: 200, NetRxBytes: 300, NetTxBytes: 300, DiskReadBytes: 300, DiskWriteBytes: 300}
		liveStatsMu.Unlock()
		collectStats(getMockClient())

		var stat db.Stat
		assert.NoError(t, db.GormDB.Where("container_id = ?", id).Order("id desc").First(&stat).Error)
		assert.Equal(t, int64(200), stat.NetRxBytes)
		assert.Equal(t, int64(200), stat.NetTxBytes)
		assert.Equal(t, int64(200), stat.DiskReadBytes)
		assert.Equal(t, int64(200), stat.DiskWriteBytes)
	})

	t.Run("happy path: spoke mode pushes to hub instead of writing SystemStat locally", func(t *testing.T) {
		assert.NoError(t, db.InitDB(":memory:"))
		origMode := LighthouseMode
		LighthouseMode = "spoke"
		t.Cleanup(func() { LighthouseMode = origMode })

		assert.NotPanics(t, func() { collectStats(getMockClient()) })

		var count int64
		db.GormDB.Model(&db.SystemStat{}).Count(&count)
		assert.Equal(t, int64(0), count)
	})
}

// ─────────────────────────────────────────────────────────────────────────
// handleGETContainers: singleflight cache-stampede guard.
// ─────────────────────────────────────────────────────────────────────────

func TestHandleGETContainers_SingleflightDedupesConcurrentCacheMiss(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	// Force a cache miss so every concurrent request below hits the
	// singleflight-guarded refill path.
	apiContainersCacheMu.Lock()
	apiContainersCache = nil
	apiContainersCacheTS = time.Time{}
	apiContainersCacheMu.Unlock()

	var callCount int32
	cli := mockDockerClientWithRoundTripper(t, func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/containers/json") {
			atomic.AddInt32(&callCount, 1)
			time.Sleep(200 * time.Millisecond) // hold the leader call open so followers can join it
			return makeResponse(http.StatusOK, `[{"Id":"sf1","Names":["/sf1"]}]`), nil
		}
		return makeResponse(http.StatusOK, `{}`), nil
	})

	const concurrency = 20
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/containers", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			mockUserContext(c, 1, true)
			_ = handleGETContainers(cli)(c)
		}()
	}
	wg.Wait()

	got := atomic.LoadInt32(&callCount)
	if got != 1 {
		t.Fatalf("ContainerList invoked %d times for %d concurrent cache-miss requests, want exactly 1 (singleflight should collapse them)", got, concurrency)
	}
}

func TestHandleGETContainers_SingleflightRecoversAfterError(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	apiContainersCacheMu.Lock()
	apiContainersCache = nil
	apiContainersCacheTS = time.Time{}
	apiContainersCacheMu.Unlock()

	cli := mockDockerClientWithRoundTripper(t, func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("simulated docker daemon unreachable")
	})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/containers", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	mockUserContext(c, 1, true)

	err = handleGETContainers(cli)(c)
	assert.NoError(t, err) // handler itself doesn't error, it writes a JSON error response
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ─────────────────────────────────────────────────────────────────────────
// handleGETSystemStats: poll-health surfacing.
// ─────────────────────────────────────────────────────────────────────────

type systemStatsPollHealthResp struct {
	PollHealthy            bool   `json:"poll_healthy"`
	LastPollError          string `json:"last_poll_error"`
	SecondsSinceLastPollOK int64  `json:"seconds_since_last_poll_ok"`
}

func TestHandleGETSystemStats_PollHealthFields(t *testing.T) {
	now := time.Now().Unix()

	tests := []struct {
		name        string
		errTs       int64
		okTs        int64
		wantHealthy bool
		wantErrMsg  bool
	}{
		{name: "happy path: never errored, never polled either", errTs: 0, okTs: 0, wantHealthy: true, wantErrMsg: false},
		{name: "happy path: succeeded after a previous error", errTs: now - 100, okTs: now - 10, wantHealthy: true, wantErrMsg: false},
		{name: "infra failure: erroring, has never once succeeded", errTs: now - 5, okTs: 0, wantHealthy: false, wantErrMsg: true},
		{name: "infra failure: most recent poll failed after an earlier success", errTs: now - 1, okTs: now - 50, wantHealthy: false, wantErrMsg: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			sysStatsMu.Lock()
			latestSystemStats = &systemStatsSnapshot{CPU: 42}
			sysStatsMu.Unlock()
			atomic.StoreInt64(&lastPollErrorUnix, tc.errTs)
			atomic.StoreInt64(&lastPollSuccessUnix, tc.okTs)

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/system/stats", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			h := handleGETSystemStats()
			err := h(c)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, rec.Code)

			var resp systemStatsPollHealthResp
			assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			assert.Equal(t, tc.wantHealthy, resp.PollHealthy)
			if tc.wantErrMsg {
				assert.NotEmpty(t, resp.LastPollError)
			} else {
				assert.Empty(t, resp.LastPollError)
			}
		})
	}
}

func TestHandleGETSystemStats_NotReady(t *testing.T) {
	sysStatsMu.Lock()
	latestSystemStats = nil
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/system/stats", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	sysStatsMu.Unlock()

	h := handleGETSystemStats()
	err := h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// ─────────────────────────────────────────────────────────────────────────
// handleGETContainersIdStats: request-context binding (leak fix).
// ─────────────────────────────────────────────────────────────────────────

func TestHandleGETContainersIdStats_ContextAlreadyCanceled(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)

	cli := mockDockerClientWithRoundTripper(t, func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/json") && !strings.Contains(req.URL.Path, "/stats") {
			return makeResponse(http.StatusOK, `{"Id":"ctxcancel","Name":"/ctxcancel","Config":{"Image":"alpine"}}`), nil
		}
		// The Docker stats call itself is still issued before the loop; only
		// the decode/encode loop is skipped once ctx is already canceled.
		return makeResponse(http.StatusOK, `{"cpu_stats":{}}`), nil
	})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/containers/ctxcancel/stats", nil)
	ctx, cancel := context.WithCancel(req.Context())
	cancel() // simulate an already-disconnected client before the handler even runs
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("ctxcancel")
	mockUserContext(c, 1, true)

	h := handleGETContainersIdStats(cli)
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Body.String(), "no stats should be written once the request context is already canceled")
}

func TestHandleGETContainersIdStats_ErrorPaths(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		userID     int
		isAdmin    bool
		seedUser   *db.User
		handler    func(req *http.Request) (*http.Response, error)
		wantStatus int
	}{
		{
			name:       "hostile: invalid container id format",
			id:         "../../etc/passwd",
			userID:     1,
			isAdmin:    true,
			handler:    func(req *http.Request) (*http.Response, error) { t.Helper(); return makeResponse(http.StatusOK, `{}`), nil },
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "infra failure: ContainerInspect fails",
			id:      "valid-id-1",
			userID:  1,
			isAdmin: true,
			handler: func(req *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("simulated docker daemon unreachable")
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:    "hostile: unauthorized non-admin user",
			id:      "othername",
			userID:  90042,
			isAdmin: false,
			seedUser: &db.User{
				ID:                 90042,
				IsRestrictedAccess: true,
				AllowedContainers:  "definitely-not-othername",
			},
			handler: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "/json") {
					return makeResponse(http.StatusOK, `{"Id":"othername","Name":"/othername","Config":{"Image":"alpine"}}`), nil
				}
				return makeResponse(http.StatusOK, `{}`), nil
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name:    "infra failure: ContainerStats fails after a successful inspect",
			id:      "valid-id-2",
			userID:  1,
			isAdmin: true,
			handler: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "/json") && !strings.Contains(req.URL.Path, "/stats") {
					return makeResponse(http.StatusOK, `{"Id":"valid-id-2","Name":"/valid-id-2","Config":{"Image":"alpine"}}`), nil
				}
				return nil, fmt.Errorf("simulated stats stream failure")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := db.InitDB(":memory:")
			assert.NoError(t, err)
			if tc.seedUser != nil {
				db.GormDB.Save(tc.seedUser)
			}

			cli := mockDockerClientWithRoundTripper(t, tc.handler)

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/containers/"+tc.id+"/stats", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues(tc.id)
			mockUserContext(c, uint(tc.userID), tc.isAdmin)

			h := handleGETContainersIdStats(cli)
			err = h(c)
			assert.NoError(t, err)
			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}
