<script setup lang="ts">
import { computed, watch } from 'vue'
import type { DeviceConfigDTO, DiscoveredDevice } from '../types/api'
import { isWwanQmiControlPath } from '../utils/deviceBackend'
import { ArrowSync24Regular, Save24Regular } from '@vicons/fluent'

const props = defineProps<{
  modelValue: boolean
  discovering: boolean
  unconfiguredDiscovered: DiscoveredDevice[]
  addSelected: DiscoveredDevice | null
  addConfig: DeviceConfigDTO
  addSaving: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  'select-device': [device: DiscoveredDevice]
  save: []
}>()

function closeDialog() {
  emit('update:modelValue', false)
}

function handleDialogModelUpdate(value: boolean) {
  emit('update:modelValue', value)
}

function discoveryIdentity(d: DiscoveredDevice | null | undefined): string {
  if (!d) return ''
  return String(d.discovery_key || `${d.usb_path || ''}|${d.at_port || ''}`)
}

function isQmiDiscovery(d: DiscoveredDevice | null | undefined): boolean {
  if (!d) return false
  return String(d.mode || '').toLowerCase() === 'qmi'
}

function discoveryModeText(d: DiscoveredDevice | null | undefined): string {
  const mode = String(d?.mode || 'unknown').toLowerCase()
  if (mode === 'qmi') return 'QMI'
  if (mode === 'mbim') return 'MBIM'
  if (mode === 'ecm') return 'ECM'
  if (mode === 'rndis') return 'RNDIS'
  if (mode === 'ncm') return 'NCM'
  return 'UNKNOWN'
}

const isQMIBackendOnly = computed(() => isWwanQmiControlPath(props.addSelected?.control_path || props.addConfig?.control_device))
const isMBIMBackendOnly = computed(
  () => String(props.addSelected?.mode || '').toLowerCase() === 'mbim'
)

watch(
  isQMIBackendOnly,
  (locked) => {
    if (locked && props.addConfig) {
      props.addConfig.device_backend = 'qmi'
    }
  },
  { immediate: true }
)

watch(
  isMBIMBackendOnly,
  (locked) => {
    if (locked && props.addConfig) {
      props.addConfig.device_backend = 'mbim'
    }
  },
  { immediate: true }
)
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    @update:model-value="handleDialogModelUpdate"
    title="添加设备配置"
    width="min(720px, 92vw)"
    class="glass-modal"
  >
    <div class="text-sm text-gray-500 mb-3">选择一个“未配置”的设备，系统将自动填充 AT 端口与识别信息。</div>
    <div class="max-h-[260px] overflow-auto space-y-2 pr-1">
      <div v-if="discovering" class="py-10 flex flex-col items-center justify-center text-gray-400">
        <el-icon class="is-loading mb-3" size="32"><ArrowSync24Regular /></el-icon>
        <div class="text-xs">正在探测设备...</div>
      </div>
      <template v-else>
        <button
          v-for="d in unconfiguredDiscovered"
          :key="discoveryIdentity(d)"
          type="button"
          class="w-full text-left p-3 rounded-xl border"
          :class="[
            d.degraded ? 'border-amber-200 bg-amber-50 cursor-not-allowed opacity-85' : '',
            !d.degraded && discoveryIdentity(addSelected) === discoveryIdentity(d) ? 'border-indigo-300 bg-indigo-50' : '',
            !d.degraded && discoveryIdentity(addSelected) !== discoveryIdentity(d) ? 'border-gray-200 hover:bg-gray-50' : ''
          ]"
          :aria-disabled="!!d.degraded"
          @click="emit('select-device', d)"
        >
          <div class="font-bold text-gray-800 flex items-center gap-2">
            <span>{{ d.net_interface || '--' }} · {{ d.driver_name || '--' }}</span>
            <el-tag size="small" :type="isQmiDiscovery(d) ? 'success' : 'warning'">{{ discoveryModeText(d) }}</el-tag>
          </div>
          <div class="text-xs text-gray-500 mt-0.5 truncate">
            {{ d.control_path }} · AT: {{ d.at_port || '--' }} · IMEI: {{ d.imei || '--' }} · USB: {{ d.usb_path || '--' }}
          </div>
          <div v-if="d.degraded" class="text-xs text-amber-700 mt-1">
            无法读取 IMEI（控制口可能挂死），暂不可添加。
          </div>
        </button>
        <div v-if="unconfiguredDiscovered.length === 0" class="text-sm text-gray-500 p-3">
          暂无可添加设备（或系统未发现新的模组）
        </div>
      </template>
    </div>

    <div v-if="addSelected" class="mt-4 p-4 bg-gray-50 border border-gray-200 rounded-xl space-y-2">
      <div class="text-xs font-bold text-gray-500 uppercase tracking-wider">选定设备状态</div>
      <div class="flex items-center gap-4 text-sm">
        <div class="flex items-center gap-2">
          <span class="text-gray-600">模式:</span>
          <el-tag size="small" :type="isQmiDiscovery(addSelected) ? 'success' : 'warning'">{{ discoveryModeText(addSelected) }}</el-tag>
          <el-tag v-if="isQMIBackendOnly" size="small" type="success">仅 QMI 后端</el-tag>
          <el-tag v-if="isMBIMBackendOnly" size="small" type="success">仅 MBIM 后端</el-tag>
        </div>
      </div>
      <div v-if="isQMIBackendOnly" class="text-xs text-emerald-700">
        此类 WWAN QMI 设备运行后端固定为 QMI；AT 口仍会保留给 AT 终端。
      </div>
    </div>

    <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 mt-4">
      <div class="space-y-1">
        <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">ID</label>
        <el-input v-model="addConfig.id" placeholder="例如 ec20_3" />
      </div>
      <div class="space-y-1">
        <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">名称</label>
        <el-input v-model="addConfig.name" placeholder="显示名称（可选）" />
      </div>
      <div class="space-y-1">
        <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">IMEI 绑定</label>
        <el-input v-model="addConfig.modem_imei" disabled placeholder="自动识别（从发现设备填充）" />
      </div>
      <div class="space-y-1">
        <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">USB 路径</label>
        <el-input v-model="addConfig.usb_path" disabled />
      </div>
      <div class="space-y-1">
        <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">网卡接口</label>
        <el-input v-model="addConfig.interface" disabled />
      </div>
      <div class="space-y-1">
        <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">AT 端口</label>
        <el-input v-model="addConfig.at_port" disabled />
      </div>
      <div class="space-y-1">
        <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">控制设备</label>
        <el-input v-model="addConfig.control_device" disabled />
      </div>
      <div class="flex items-center justify-between p-3 rounded-xl border border-gray-200 bg-gray-50">
        <div>
          <div class="text-sm font-bold text-gray-800">设备后端模式</div>
          <div class="text-xs text-gray-500">
            {{ isQMIBackendOnly ? '固定 QMI，AT 口仅用于终端'
               : (isMBIMBackendOnly ? '固定 MBIM，AT 口仅用于终端'
               : 'AT=串口 / QMI=纯 QMI') }}
          </div>
        </div>
        <el-select
          v-model="addConfig.device_backend"
          style="width: 110px"
          placeholder="AT"
          :disabled="isQMIBackendOnly || isMBIMBackendOnly"
        >
          <el-option v-if="!isMBIMBackendOnly" label="AT" value="at" :disabled="isQMIBackendOnly" />
          <el-option v-if="!isMBIMBackendOnly" label="QMI" value="qmi" :disabled="!addConfig.control_device" />
          <el-option v-if="isMBIMBackendOnly" label="MBIM" value="mbim" />
        </el-select>
      </div>
      <div class="flex items-center justify-between p-3 rounded-xl border border-gray-200 bg-gray-50">
        <div>
          <div class="text-sm font-bold text-gray-800">模组厂商</div>
          <div class="text-xs text-gray-500">Quectel / SIMCOM</div>
        </div>
        <el-select
          v-model="addConfig.module_vendor"
          style="width: 110px"
          placeholder="Quectel"
        >
          <el-option label="Quectel" value="quectel" />
          <el-option label="SIMCOM" value="simcom" />
        </el-select>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-2">
        <el-button @click="closeDialog" class="ui-button-plain">取消</el-button>
        <el-button type="primary" :loading="addSaving" @click="emit('save')" class="!border-0">
          <el-icon><Save24Regular /></el-icon>
          保存
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>
