<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { Phone24Regular } from '@vicons/fluent'
import { devicesService } from '../services/devices'

type UssdRealtimeEvent = {
  nonce?: number
  device_id?: string
  session_id?: string
  text?: string
  raw_text?: string
  status?: number
  dcs?: number
  done?: boolean
  channel?: string
}

type UssdDisplayResult = {
  status?: number
  text?: string
  rawText?: string
  dcs?: number
  sessionId?: string
  channel?: string
  done?: boolean
}

const props = defineProps<{
  deviceId: string
  vowifiActive?: boolean
  ussdEvent?: UssdRealtimeEvent | null
}>()

const ussdCmd = ref('')
const ussdTimeoutMs = ref(45000)
const sending = ref(false)
const sessionId = ref('')
const sessionChannel = ref('')
const history = ref<Array<{ ts: number; type: 'req' | 'res' | 'err' | 'sys'; content: string; dcs?: number; channel?: string }>>([])
const responseSeen = new Map<string, number>()

const isMultiRound = computed(() => !!sessionId.value)
const inputPlaceholder = computed(() => isMultiRound.value ? '输入菜单选项数字' : '例如 *100# 或菜单回复数字')

function responseContent(d: UssdDisplayResult) {
  return d.text || d.rawText || '[空响应]'
}

function responseKey(d: UssdDisplayResult) {
  return [
    d.sessionId || '',
    d.channel || '',
    d.status ?? '',
    d.dcs ?? '',
    d.done === undefined ? '' : String(d.done),
    responseContent(d)
  ].join('\u0000')
}

function isDuplicateResponse(d: UssdDisplayResult) {
  const now = Date.now()
  for (const [key, ts] of responseSeen) {
    if (now - ts > 5000) responseSeen.delete(key)
  }
  const key = responseKey(d)
  const seenAt = responseSeen.get(key)
  if (seenAt && now - seenAt <= 5000) return true
  responseSeen.set(key, now)
  return false
}

function wantsMoreInput(d: UssdDisplayResult) {
  if (!d.sessionId) return false
  if (d.status === 1) return true
  if (d.status === 2 || d.status === 5) return false
  return d.done === false
}

function updateSession(d: UssdDisplayResult) {
  if (d.channel) sessionChannel.value = d.channel
  if (wantsMoreInput(d)) {
    sessionId.value = d.sessionId || ''
    return
  }
  endSession()
}

function appendUSSDResponse(d: UssdDisplayResult) {
  const duplicate = isDuplicateResponse(d)
  updateSession(d)
  if (duplicate) return

  if (d.status === 5) {
    history.value.push({
      ts: Date.now(),
      type: 'err',
      content: `[网络不支持/无响应]\n` + responseContent(d),
      dcs: d.dcs,
      channel: d.channel
    })
    return
  }
  if (d.status === 2) {
    history.value.push({
      ts: Date.now(),
      type: 'err',
      content: `[被网络终止]\n` + responseContent(d),
      dcs: d.dcs,
      channel: d.channel
    })
    return
  }
  history.value.push({
    ts: Date.now(),
    type: 'res',
    content: responseContent(d),
    dcs: d.dcs,
    channel: d.channel
  })
}

watch(() => props.ussdEvent, (ev) => {
  if (!ev) return
  const eventDeviceID = String(ev.device_id || '').trim()
  if (eventDeviceID && eventDeviceID !== props.deviceId) return
  if (!ev.session_id && !ev.text && !ev.raw_text) return
  appendUSSDResponse({
    status: ev.status,
    text: ev.text || '',
    rawText: ev.raw_text || '',
    dcs: ev.dcs,
    sessionId: ev.session_id || '',
    channel: ev.channel || 'vowifi',
    done: ev.done
  })
})

async function sendUSSD() {
  const cmd = String(ussdCmd.value || '').trim()
  if (!cmd) return
  
  history.value.push({ ts: Date.now(), type: 'req', content: cmd })
  sending.value = true
  ussdCmd.value = ''

  try {
    let d: UssdDisplayResult

    if (isMultiRound.value) {
      // 多轮模式：通过 continue 接口发送后续输入
      const result = await devicesService.continueUSSD(props.deviceId, {
        session_id: sessionId.value,
        input: cmd,
        timeout_ms: ussdTimeoutMs.value || 45000
      }, (ussdTimeoutMs.value || 45000) + 2000)
      if (!result.ok) throw new Error(result.error.message || '请求异常')
      d = result.data
    } else {
      // 首轮模式：发起新 USSD 请求
      const result = await devicesService.sendUSSD(props.deviceId, {
        command: cmd,
        timeout_ms: ussdTimeoutMs.value || 45000
      }, (ussdTimeoutMs.value || 45000) + 2000)
      if (!result.ok) throw new Error(result.error.message || '请求异常')
      d = result.data
    }

    appendUSSDResponse(d)
  } catch (e: unknown) {
    history.value.push({
      ts: Date.now(),
      type: 'err',
      content: e instanceof Error ? e.message : '请求异常'
    })
    endSession()
  } finally {
    sending.value = false
  }
}

async function cancelSession() {
  if (!sessionId.value) return
  try {
    await devicesService.cancelUSSD(props.deviceId, sessionId.value)
    history.value.push({
      ts: Date.now(),
      type: 'sys',
      content: '会话已手动取消'
    })
  } catch {
    // 忽略取消错误
  }
  endSession()
}

function endSession() {
  sessionId.value = ''
  sessionChannel.value = ''
}

function clearHistory() {
  history.value = []
  responseSeen.clear()
  endSession()
}
</script>

<template>
  <div>
    <div class="flex items-center gap-3">
      <div class="w-10 h-10 rounded-xl bg-gray-100 dark:bg-gray-800 flex items-center justify-center text-gray-700 dark:text-gray-300">
        <el-icon size="22"><Phone24Regular /></el-icon>
      </div>
      <div class="flex-1">
        <div class="text-lg font-bold text-gray-900 dark:text-white">USSD 交互终端</div>
        <div class="text-sm text-gray-500 dark:text-gray-400 mt-0.5">发送 USSD 代码 (如 *100#) 并等待网络菜单响应</div>
      </div>
      <!-- 多轮会话状态指示 -->
      <div v-if="isMultiRound" class="flex items-center gap-2">
        <span class="inline-flex items-center gap-1.5 text-xs font-medium text-emerald-600 dark:text-emerald-400 bg-emerald-50 dark:bg-emerald-900/30 border border-emerald-200 dark:border-emerald-800 px-2.5 py-1 rounded-full">
          <span class="w-1.5 h-1.5 bg-emerald-500 rounded-full animate-pulse"></span>
          多轮会话中
        </span>
        <el-button size="small" type="warning" plain @click="cancelSession" :disabled="sending">取消会话</el-button>
      </div>
    </div>

    <!-- 交互历史面板 -->
    <div class="ui-panel-muted mt-4 p-4 h-[320px] overflow-auto flex flex-col gap-3 rounded-xl border border-gray-100 dark:border-white/10 relative">
      <div v-if="history.length === 0 && !sending" class="absolute inset-0 flex items-center justify-center text-sm text-gray-400">
        暂无 USSD 会话记录
      </div>
      <div v-for="(msg, i) in history" :key="i" class="flex w-full" :class="msg.type === 'req' ? 'justify-end' : 'justify-start'">
        <!-- 请求记录（右侧气泡） -->
        <div v-if="msg.type === 'req'" class="max-w-[80%] bg-indigo-500 text-white rounded-2xl rounded-tr-sm px-4 py-2.5 shadow-sm">
          <div class="text-sm break-words">{{ msg.content }}</div>
          <div class="text-[10px] text-indigo-100 mt-1 text-right">{{ new Date(msg.ts).toLocaleTimeString() }}</div>
        </div>
        
        <!-- 系统消息（居中） -->
        <div v-else-if="msg.type === 'sys'" class="w-full text-center">
          <span class="inline-block text-xs text-gray-400 bg-gray-100 dark:bg-gray-800 px-3 py-1 rounded-full">{{ msg.content }}</span>
        </div>

        <!-- 响应/错误记录（左侧气泡） -->
        <div v-else class="max-w-[80%] rounded-2xl rounded-tl-sm px-4 py-2.5 shadow-sm" :class="msg.type === 'err' ? 'bg-red-50 dark:bg-red-900/30 text-red-700 dark:text-red-300 border border-red-100 dark:border-red-900/50' : 'bg-white dark:bg-gray-800 text-gray-800 dark:text-gray-200 border border-gray-100 dark:border-white/5'">
          <div class="text-sm whitespace-pre-wrap break-words font-mono">{{ msg.content }}</div>
          <div class="text-[10px] mt-1 text-gray-400 flex items-center gap-2">
            <span>{{ new Date(msg.ts).toLocaleTimeString() }}</span>
            <span v-if="msg.dcs !== undefined" class="bg-gray-100 dark:bg-gray-700 px-1 rounded">DCS: {{ msg.dcs }}</span>
            <span v-if="msg.channel" class="bg-gray-100 dark:bg-gray-700 px-1 rounded">{{ msg.channel === 'vowifi' ? 'VoWiFi' : 'CS' }}</span>
          </div>
        </div>
      </div>
      <!-- 发送中等待状态（左侧呼吸气泡） -->
      <div v-if="sending" class="flex w-full justify-start mt-2">
        <div class="max-w-[80%] bg-white dark:bg-gray-800 rounded-2xl rounded-tl-sm px-4 py-3 shadow-sm border border-gray-100 dark:border-white/5 flex items-center gap-2">
          <div class="flex space-x-1">
            <div class="w-1.5 h-1.5 bg-indigo-400 rounded-full animate-bounce [animation-delay:-0.3s]"></div>
            <div class="w-1.5 h-1.5 bg-indigo-400 rounded-full animate-bounce [animation-delay:-0.15s]"></div>
            <div class="w-1.5 h-1.5 bg-indigo-400 rounded-full animate-bounce"></div>
          </div>
          <span class="text-xs text-gray-400 ml-1">等待网络响应...</span>
        </div>
      </div>
    </div>

    <!-- 输入区 -->
    <div class="grid grid-cols-1 md:grid-cols-[1fr_110px_auto] gap-3 mt-4">
      <div class="space-y-1">
        <div class="text-[11px] font-bold text-gray-500 uppercase tracking-wider">
          {{ isMultiRound ? '菜单回复' : '命令 / 回复' }}
        </div>
        <el-input
          v-model="ussdCmd"
          :placeholder="inputPlaceholder"
          @keyup.enter="sendUSSD"
          :disabled="sending"
        />
      </div>
      <div class="space-y-1">
        <div class="text-[11px] font-bold text-gray-500 uppercase tracking-wider">超时(ms)</div>
        <el-input v-model.number="ussdTimeoutMs" type="number" inputmode="numeric" placeholder="45000" />
      </div>
      <div class="space-y-1 self-end">
        <div class="text-[11px] font-bold text-gray-500 uppercase tracking-wider opacity-0 select-none">操作</div>
        <div class="flex items-center justify-end gap-2">
          <el-button type="default" @click="clearHistory" class="ui-button-plain">清空</el-button>
          <el-button type="primary" :loading="sending" :disabled="!ussdCmd" @click="sendUSSD" class="!border-0">
            {{ isMultiRound ? '回复' : '发送' }}
          </el-button>
        </div>
      </div>
    </div>
  </div>
</template>
