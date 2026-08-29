import { ref, computed, onMounted, onUnmounted } from 'vue';
import { useRouter } from 'vue-router';
import { secureStorage } from '../utils/storage';
import { sharedState, showToast, formatBytes } from '../utils/sharedState';
import { apiFetch } from '../utils/apiFetch';

// This composable centralizes the container list: a single module-level
// `containers` ref shared by every component that calls useContainers(), kept
// fresh by one shared 8-second poll (ref-counted via pollSubscribers) so
// multiple mounted views never each start their own duplicate polling loop.
const containers = ref([]);
const loading = ref(true);
let pollInterval = null;
let pollSubscribers = 0;

// fetchContainers loads the current container list from the backend into the
// shared `containers` ref.
export async function fetchContainers() {
  try {
    const token = secureStorage.getItem('token');
    const res = await apiFetch('/api/containers', {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (res.ok) {
      containers.value = await res.json();
    }
  } catch (err) {
    console.error(err); showToast('Error', 'An error occurred. Check console for details.', 'error');
  } finally {
    loading.value = false;
  }
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
        showToast('Error', 'Action failed.', 'error');
      }
    } catch (err) {
      console.error(err); showToast('Error', 'An error occurred. Check console for details.', 'error');
      showToast('Error', 'Action failed.', 'error');
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
