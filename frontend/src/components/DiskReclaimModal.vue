<template>
  <div class="modal-overlay" @click.self="$emit('close')">
    <div class="modal-content shadow-2xl prune-modal animate-fade-in">
      <div class="modal-header">
        <div class="prune-title-wrapper">
          <div class="prune-icon">
            <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="2.5">
              <polyline points="3 6 5 6 21 6"></polyline>
              <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
              <line x1="10" y1="11" x2="10" y2="17"></line>
              <line x1="14" y1="11" x2="14" y2="17"></line>
            </svg>
          </div>
          <div>
            <h3>Disk Reclaim & System Prune</h3>
            <span class="prune-sub">Safely clean up stopped containers, dangling images, and build cache</span>
          </div>
        </div>
        <button class="close-btn" @click="$emit('close')">✕</button>
      </div>

      <div class="modal-body prune-body">
        <div v-if="loading" class="loading-state">
          <div class="spinner"></div>
          <span>Analyzing host storage...</span>
        </div>

        <template v-else-if="dfData">
          <!-- Total reclaimable banner -->
          <div class="total-reclaim-banner">
            <div class="banner-info">
              <span class="banner-lbl">Total Safe Reclaimable Space</span>
              <h2 class="banner-val">{{ formatBytes(dfData.total_reclaimable) }}</h2>
            </div>
            <div class="banner-layers">
              <span>Host Layer Cache: {{ formatBytes(dfData.layers_size) }}</span>
            </div>
          </div>

          <!-- Target Selection List -->
          <div class="targets-list">
            <!-- 1. Containers -->
            <label class="target-card" :class="{ selected: targets.containers }">
              <div class="target-left">
                <input type="checkbox" v-model="targets.containers" />
                <div class="target-text">
                  <strong>Stopped Containers</strong>
                  <span class="target-desc">{{ dfData.containers_count }} containers total</span>
                </div>
              </div>
              <span class="target-size">{{ formatBytes(dfData.containers_size) }}</span>
            </label>

            <!-- 2. Images -->
            <label class="target-card" :class="{ selected: targets.images }">
              <div class="target-left">
                <input type="checkbox" v-model="targets.images" />
                <div class="target-text">
                  <strong>Unused & Dangling Images</strong>
                  <span class="target-desc">{{ dfData.images_count }} images stored</span>
                </div>
              </div>
              <span class="target-size reclaimable">{{ formatBytes(dfData.images_reclaimable) }}</span>
            </label>

            <!-- 3. Volumes -->
            <label class="target-card" :class="{ selected: targets.volumes }">
              <div class="target-left">
                <input type="checkbox" v-model="targets.volumes" />
                <div class="target-text">
                  <strong>Unused Docker Volumes</strong>
                  <span class="target-desc">{{ dfData.volumes_count }} volumes mounted</span>
                </div>
              </div>
              <span class="target-size reclaimable">{{ formatBytes(dfData.volumes_reclaimable) }}</span>
            </label>

            <!-- 4. Build Cache -->
            <label class="target-card" :class="{ selected: targets.build_cache }">
              <div class="target-left">
                <input type="checkbox" v-model="targets.build_cache" />
                <div class="target-text">
                  <strong>Docker Build Cache</strong>
                  <span class="target-desc">{{ dfData.build_cache_count }} cache records</span>
                </div>
              </div>
              <span class="target-size reclaimable">{{ formatBytes(dfData.build_cache_reclaimable) }}</span>
            </label>
          </div>

          <!-- All images toggle -->
          <div class="all-images-toggle">
            <label style="display: flex; align-items: center; gap: 0.5rem; font-size: 0.8rem; color: #94a3b8; cursor: pointer;">
              <input type="checkbox" v-model="pruneAllImages" />
              <span>Prune all unused images (not just dangling untagged images)</span>
            </label>
          </div>
        </template>
      </div>

      <div class="modal-actions">
        <button class="modal-btn cancel" @click="$emit('close')" :disabled="pruning">
          Cancel
        </button>
        <button
          class="modal-btn confirm danger"
          @click="executePrune"
          :disabled="pruning || !hasAnyTargetSelected"
        >
          {{ pruning ? 'Reclaiming Disk Space...' : 'Clean Up Selected Resources' }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from "vue";
import { apiFetch } from "../utils/apiFetch";

const emit = defineEmits(["close", "reclaimed"]);

const loading = ref(true);
const pruning = ref(false);
const dfData = ref(null);
const pruneAllImages = ref(false);

const targets = ref({
  containers: true,
  images: true,
  volumes: false,
  build_cache: true
});

const hasAnyTargetSelected = computed(() => {
  return (
    targets.value.containers ||
    targets.value.images ||
    targets.value.volumes ||
    targets.value.build_cache
  );
});

const formatBytes = (bytes) => {
  if (!bytes || bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + " " + sizes[i];
};

const fetchDF = async () => {
  loading.value = true;
  try {
    const res = await apiFetch("/api/system/df");
    if (res.ok) {
      dfData.value = await res.json();
    }
  } catch (err) {
    console.error("Failed to query disk usage:", err);
  } finally {
    loading.value = false;
  }
};

const executePrune = async () => {
  if (!confirm("Proceed with disk reclaim? Unused targets will be pruned.")) return;
  pruning.value = true;
  try {
    const res = await apiFetch("/api/system/prune", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        prune_containers: targets.value.containers,
        prune_images: targets.value.images,
        prune_volumes: targets.value.volumes,
        prune_build_cache: targets.value.build_cache,
        all: pruneAllImages.value
      })
    });
    const data = await res.json();
    if (res.ok) {
      alert(`Disk reclaim complete! Reclaimed ${formatBytes(data.space_reclaimed_bytes)} of disk space.`);
      emit("reclaimed", data);
      emit("close");
    } else {
      alert("Prune failed: " + (data.error || res.statusText));
    }
  } catch (err) {
    alert("Prune request error: " + err.message);
  } finally {
    pruning.value = false;
  }
};

onMounted(() => {
  fetchDF();
});
</script>

<style scoped>
.prune-modal {
  width: 580px;
  max-width: 95vw;
}

.prune-title-wrapper {
  display: flex;
  gap: 0.75rem;
  align-items: center;
}

.prune-icon {
  width: 42px;
  height: 42px;
  border-radius: 10px;
  background: rgba(239, 68, 68, 0.15);
  color: #ef4444;
  display: flex;
  align-items: center;
  justify-content: center;
}

.prune-title-wrapper h3 {
  margin: 0;
  font-size: 1.15rem;
}

.prune-sub {
  font-size: 0.75rem;
  color: #94a3b8;
}

.total-reclaim-banner {
  background: linear-gradient(135deg, rgba(16, 185, 129, 0.15), rgba(59, 130, 246, 0.15));
  border: 1px solid rgba(16, 185, 129, 0.3);
  border-radius: 10px;
  padding: 1.25rem;
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 1.25rem;
}

.banner-lbl {
  font-size: 0.8rem;
  color: #a7f3d0;
  text-transform: uppercase;
  font-weight: 600;
  letter-spacing: 0.5px;
}

.banner-val {
  margin: 0.25rem 0 0 0;
  font-size: 1.8rem;
  font-weight: 700;
  color: #10b981;
}

.banner-layers {
  font-size: 0.8rem;
  color: #94a3b8;
}

.targets-list {
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
  margin-bottom: 1rem;
}

.target-card {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.85rem 1rem;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.08);
  cursor: pointer;
  transition: all 0.15s ease;
}

.target-card:hover {
  background: rgba(255, 255, 255, 0.06);
}

.target-card.selected {
  border-color: rgba(59, 130, 246, 0.5);
  background: rgba(59, 130, 246, 0.08);
}

.target-left {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.target-text {
  display: flex;
  flex-direction: column;
}

.target-text strong {
  font-size: 0.9rem;
}

.target-desc {
  font-size: 0.75rem;
  color: #94a3b8;
}

.target-size {
  font-size: 0.9rem;
  font-weight: 600;
}

.target-size.reclaimable {
  color: #10b981;
}

.all-images-toggle {
  background: rgba(0, 0, 0, 0.2);
  padding: 0.6rem 0.8rem;
  border-radius: 6px;
}
</style>
