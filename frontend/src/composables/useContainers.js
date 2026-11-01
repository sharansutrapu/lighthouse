import { ref, computed, onMounted, onUnmounted } from 'vue';
import { useRouter } from 'vue-router';
import { secureStorage } from '../utils/storage';
import { sharedState, showToast, formatBytes } from '../utils/sharedState';
import { apiFetch, apiStatus, getAPIError } from '../utils/apiFetch';

// This composable centralizes the container list: a single module-level
// `containers` ref shared by every component that calls useContainers(), kept
// fresh by one shared 8-second poll (ref-counted via pollSubscribers) so
// multiple mounted views never each start their own duplicate polling loop.
const containers = ref([]);
const loading = ref(true);
let pollInterval = null;
let pollSubscribers = 0;
let fetchPromise = null;
let fetchController = null;
let activeToken = null;

// fetchContainers loads the current container list from the backend into the
// shared `containers` ref.
export function fetchContainers() {
  const token = secureStorage.getItem('token');
  if (token !== activeToken) {
    fetchController?.abort();
    fetchPromise = null;
    containers.value = [];
    loading.value = true;
    activeToken = token;
  }
  if (!token) {
    loading.value = false;
    return Promise.resolve();
  }
  if (fetchPromise) return fetchPromise;
  const controller = new AbortController();
  fetchController = controller;
  fetchPromise = (async () => {
    try {
      const res = await apiFetch('/api/containers', {
        headers: { Authorization: `Bearer ${token}` },
        signal: controller.signal,
      });
      if (res.ok) {
        const data = await res.json();
        if (!controller.signal.aborted && secureStorage.getItem('token') === token) {
          containers.value = Array.isArray(data) ? data : [];
        }
      }
    } catch (error) {
      if (error.name !== 'AbortError' && secureStorage.getItem('token') === token) {
        apiStatus['/api/containers'] = { unavailableNodes: [], error: 'Container inventory could not be refreshed.' };
      }
    } finally {
      if (fetchController === controller) {
        loading.value = false;
        fetchController = null;
        fetchPromise = null;
      }
    }
  })();
  return fetchPromise;
}

// startPolling begins the shared 8-second refresh loop (idempotent — safe to
// call while already running).
function startPolling() {
  if (pollInterval) return;
  fetchContainers();
  pollInterval = setInterval(fetchContainers, 8000);
}

// stopPolling halts the shared refresh loop.
function stopPolling() {
  if (pollInterval) {
    clearInterval(pollInterval);
    pollInterval = null;
  }
  fetchController?.abort();
  fetchController = null;
  fetchPromise = null;
}

// formatContainerDate renders a Unix timestamp (seconds) as a locale date/time string.
export function formatContainerDate(unix) {
  if (!unix) return 'N/A';
  return new Date(unix * 1000).toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

// useContainers exposes the shared container list plus start/stop/restart/
// remove actions (with a confirmation-modal flow) to any component. Pass
// { autoPoll: false } to read the shared state without joining the polling loop.
export function useContainers(options = {}) {
  const { autoPoll = true } = options;
  const router = useRouter();
  if (secureStorage.getItem('token') !== activeToken) containers.value = [];

  const showConfirm = ref(false);
  const pendingId = ref(null);
  const pendingAction = ref('');
  const isActionLoading = ref(false);

  const actionClass = computed(() => {
    if (pendingAction.value === 'start') return 'success';
    if (pendingAction.value === 'restart') return 'warning';
    if (pendingAction.value === 'stop' || pendingAction.value === 'remove') return 'error';
    return '';
  });



  const runningCount = computed(
    () => containers.value.filter((c) => c.state === 'running').length,
  );

  const stoppedCount = computed(
    () => containers.value.filter((c) => c.state !== 'running').length,
  );


  // goToLogs/goToShell/goToDetail navigate to the Logs/Shell/detail page for
  // a given container ID.
  const goToLogs = (id) => {
    router.push({ path: '/logs', query: { c: id } });
  };

  const goToShell = (id) => {
    router.push({ path: '/shell', query: { c: id } });
  };

  const goToDetail = (id) => {
    router.push({ path: `/containers/${id}` });
  };

  // triggerConfirm opens the shared confirmation modal for a pending
  // start/stop/restart/remove action.
  const triggerConfirm = (id, action) => {
    pendingId.value = id;
    pendingAction.value = action;
    showConfirm.value = true;
  };

  // executeAction runs the confirmed pending action against the backend and
  // refreshes the container list on success.
  const executeAction = async () => {
    if (!pendingId.value || !pendingAction.value) return;
    if (isActionLoading.value) return;
    isActionLoading.value = true;
    showConfirm.value = false; // close modal immediately
    try {
      const token = secureStorage.getItem('token');
      const formData = new FormData();
      formData.append('action', pendingAction.value);
      const res = await apiFetch(`/api/containers/${pendingId.value}/action`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` },
        body: formData,
      });
      if (res.ok) {
        showToast('Success', `Action ${pendingAction.value} executed.`, 'success');
        await fetchContainers();
      } else {
        showToast('Error', await getAPIError(res, 'Container action failed.'), 'error');
      }
    } catch (err) {
      showToast('Error', 'Container action failed. The server could not be reached.', 'error');
    } finally {
      isActionLoading.value = false;
    }
  };

  onMounted(() => {
    if (!autoPoll) return;
    pollSubscribers += 1;
    if (pollSubscribers === 1) startPolling();
  });

  onUnmounted(() => {
    if (!autoPoll) return;
    pollSubscribers -= 1;
    if (pollSubscribers === 0) stopPolling();
  });

  return {
    containers,
    loading,

    runningCount,
    stoppedCount,
    showConfirm,
    pendingAction,
    actionClass,
    isActionLoading,
    fetchContainers,
    goToLogs,
    goToShell,
    goToDetail,
    triggerConfirm,
    executeAction,
    formatBytes,
    formatDate: formatContainerDate,
  };
}
