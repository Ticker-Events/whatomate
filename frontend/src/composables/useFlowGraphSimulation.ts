import { reactive, computed, ref, type Ref } from 'vue'
import type { ChatFlowGraph, ChatNode } from '@/services/api'
import type {
  FlowData,
  SimulationState,
  SimulationMessage,
  ExecutionLogType,
  ButtonConfig,
  PreviewCarouselCard,
  UserInput,
} from '@/types/flow-preview'
import { useApiMocker } from './useApiMocker'

function generateId(): string {
  return Math.random().toString(36).substring(2, 9)
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

const FOR_RE = /\{\{\s*for\s+([A-Za-z_][A-Za-z0-9_]*)\s+in\s+([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)\s*\}\}([\s\S]*?)\{\{\s*endfor\s*\}\}/
const VAR_RE = /\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*|\[\d+\])*)\s*\}\}/g
const MAX_LOOP_ITEMS = 50

// Mirrors the backend processTemplate: {{for item in items}}...{{endfor}},
// then {{variable}} / {{object.field}} / {{items[0].name}}.
function lookupPath(vars: Record<string, any>, path: string): unknown {
  let current: unknown = vars
  for (const part of path.trim().split('.')) {
    if (current == null || typeof current !== 'object') return undefined
    const arrayMatch = part.match(/^([A-Za-z_][A-Za-z0-9_]*)\[(\d+)\]$/)
    if (arrayMatch) {
      const list = (current as Record<string, unknown>)[arrayMatch[1]]
      if (!Array.isArray(list)) return undefined
      current = list[Number(arrayMatch[2])]
      continue
    }
    current = (current as Record<string, unknown>)[part]
  }
  return current
}

function replaceVariables(template: string, vars: Record<string, any>): string {
  return template.replace(VAR_RE, (_, path: string) => {
    const value = lookupPath(vars, path)
    if (value == null) return ''
    if (typeof value === 'object') return ''
    return String(value)
  })
}

function interpolate(template: string, vars: Record<string, any>): string {
  if (!template) return ''
  let result = template
  for (let pass = 0; pass < MAX_LOOP_ITEMS; pass++) {
    const match = FOR_RE.exec(result)
    if (!match) break
    const itemVar = match[1]
    const body = match[3]
    const arrayValue = lookupPath(vars, match[2])
    let rendered = ''
    if (Array.isArray(arrayValue)) {
      const parts: string[] = []
      const limit = Math.min(arrayValue.length, MAX_LOOP_ITEMS)
      for (let i = 0; i < limit; i++) {
        parts.push(replaceVariables(body, {
          ...vars,
          [itemVar]: arrayValue[i],
          [`${itemVar}_index`]: i,
        }))
      }
      rendered = parts.join('')
    }
    result = result.slice(0, match.index) + rendered + result.slice(match.index + match[0].length)
  }
  return replaceVariables(result, vars)
}

/**
 * useFlowGraphSimulation walks a v2 ChatFlowGraph node by node, mirroring
 * the backend runChatGraph executor. It powers the in-browser flow preview
 * so authors can simulate exactly the runtime the server uses.
 *
 * The exposed shape intentionally matches the legacy useFlowSimulation
 * (state, currentStep, isWaitingForInput, expectedInputType, actions)
 * so InteractivePreview can swap composables with minimal churn.
 */
export function useFlowGraphSimulation(
  graph: Ref<ChatFlowGraph | null>,
  flowData: Ref<Partial<FlowData>>,
) {
  const apiMocker = useApiMocker()

  const state = reactive<SimulationState>({
    mode: 'preview',
    status: 'idle',
    currentStepIndex: null,
    currentStepName: null,
    variables: {},
    messages: [],
    history: [],
    historyIndex: -1,
    currentRetryCount: 0,
    executionLog: [],
    apiMocks: {},
  })

  // currentStepName actually holds the node id during graph runs. The
  // legacy bindings in InteractivePreview reference `state.currentStepName`
  // and `state.currentStepIndex` — we surface the equivalents below.
  const currentNode = computed<ChatNode | null>(() => {
    const g = graph.value
    if (!g || !state.currentStepName) return null
    return g.nodes.find((n) => n.id === state.currentStepName) || null
  })

  // Legacy alias used by InteractivePreview templates that reference
  // currentStep.message_type / .input_config / etc. We surface a thin
  // adapter so the existing template bindings keep rendering.
  const currentStep = computed(() => {
    const node = currentNode.value
    if (!node) return null
    return adaptNodeAsStep(node)
  })

  const isWaitingForInput = computed(() => state.status === 'waiting_input')

  const expectedInputType = computed<string | null>(() => {
    const node = currentNode.value
    if (!node) return null
    switch (node.type) {
      case 'buttons':
        return 'button'
      case 'whatsapp_flow':
        return 'whatsapp_flow'
      case 'prompt':
        return 'text'
      default:
        return null
    }
  })

  // History of state snapshots taken before each node executes, so the
  // user can step backwards through a simulation.
  const snapshots = ref<string[]>([])
  const canUndo = computed(() => snapshots.value.length > 0)

  function snapshot(): void {
    snapshots.value.push(JSON.stringify({
      status: state.status,
      currentStepIndex: state.currentStepIndex,
      currentStepName: state.currentStepName,
      variables: state.variables,
      messages: state.messages,
      executionLog: state.executionLog,
      currentRetryCount: state.currentRetryCount,
    }))
    if (snapshots.value.length > 50) snapshots.value.shift()
  }

  function log(type: ExecutionLogType, nodeId?: string, details: Record<string, any> = {}): void {
    state.executionLog.push({
      id: generateId(),
      timestamp: new Date(),
      type,
      stepName: nodeId,
      details,
    })
    if (state.executionLog.length > 200) {
      state.executionLog = state.executionLog.slice(-200)
    }
  }

  function addMessage(
    type: SimulationMessage['type'],
    content: string,
    options: Partial<SimulationMessage> = {},
  ): void {
    state.messages.push({
      id: generateId(),
      type,
      content,
      timestamp: new Date(),
      ...options,
    })
  }

  function setVariable(key: string, value: any): void {
    state.variables[key] = value
    log('variable_set', state.currentStepName || undefined, { key, value })
  }

  // ---- Edge resolution ---------------------------------------------------

  function resolveEdge(fromId: string, outcome: string): string {
    const g = graph.value
    if (!g) return ''
    let fallback = ''
    for (const e of g.edges) {
      if (e.from !== fromId) continue
      if (e.condition === outcome) return e.to
      if (e.condition === 'default') fallback = e.to
    }
    return fallback
  }

  function nodeById(id: string): ChatNode | null {
    return graph.value?.nodes.find((n) => n.id === id) || null
  }

  // ---- Lifecycle ---------------------------------------------------------

  async function startSimulation(): Promise<void> {
    const g = graph.value
    if (!g || g.nodes.length === 0) {
      state.status = 'error'
      state.errorMessage = 'Flow has no v2 graph'
      return
    }

    state.status = 'running'
    // Seed built-in template variables so preview matches what the
    // backend's runChatGraph will produce (phone_number is always
    // populated server-side from the session).
    state.variables = { phone_number: '+15555550100', contact_name: 'Preview User' }
    state.messages = []
    state.executionLog = []
    state.currentRetryCount = 0
    snapshots.value = []

    log('flow_start', undefined, { nodeCount: g.nodes.length })

    if (flowData.value.initial_message) {
      addMessage('bot', flowData.value.initial_message)
      await delay(300)
    }

    state.currentStepName = g.entry_node
    state.currentStepIndex = g.nodes.findIndex((n) => n.id === g.entry_node)
    if (state.currentStepIndex < 0) state.currentStepIndex = 0
    await runFrom(g.entry_node)
  }

  // runFrom keeps stepping through non-yielding nodes until it hits a
  // yielding one (buttons / prompt / whatsapp_flow / end / transfer) or
  // walks off the graph.
  async function runFrom(startId: string): Promise<void> {
    let currentId = startId
    let safety = 100

    while (currentId && safety-- > 0) {
      const node = nodeById(currentId)
      if (!node) {
        addMessage('system', `Node "${currentId}" not found`)
        state.status = 'error'
        return
      }
      // Snapshot before executing this node so Undo can rewind to it.
      snapshot()
      state.currentStepName = currentId
      state.currentStepIndex = graph.value?.nodes.findIndex((n) => n.id === currentId) ?? null
      log('step_enter', currentId, { type: node.type })

      const outcome = await execute(node)
      if (outcome === '__yield__') return
      if (outcome === '__end__') {
        complete()
        return
      }

      const next = resolveEdge(currentId, outcome || 'default')
      log('step_exit', currentId, { outcome, next })
      if (!next) {
        complete()
        return
      }
      currentId = next
      await delay(150)
    }

    if (safety <= 0) {
      addMessage('system', 'Aborted: too many non-blocking nodes (cycle?)')
      state.status = 'error'
    }
  }

  // ---- Per-node executors ------------------------------------------------

  async function execute(node: ChatNode): Promise<string> {
    switch (node.type) {
      case 'start':
        // Entry sentinel — mirror backend ChatNodeStart (no side effect).
        return 'default'
      case 'message':
        return execMessage(node)
      case 'buttons':
        return execButtons(node)
      case 'end':
        return execEnd(node)
      case 'condition':
        return execCondition(node)
      case 'timing':
        return execTiming(node)
      case 'prompt':
        return execPrompt(node)
      case 'set_variable':
        return execSetVariable(node)
      case 'transfer':
        return execTransfer(node)
      case 'api_call':
        return execApiCall(node)
      case 'tiqr_store_api':
        return execTiqrStoreApi(node)
      case 'whatsapp_flow':
        return execWhatsAppFlow(node)
      case 'goto_flow':
        // In a single-flow preview we can't actually jump; show a system note.
        addMessage('system', `[goto_flow] would jump to flow ${node.config?.flow_id || '?'}`)
        return '__end__'
      case 'ai_response':
        addMessage('system', '[ai_response] simulated — backend AI is not invoked in preview')
        return 'default'
      case 'webhook':
        addMessage('system', '[webhook] simulated — request not sent in preview')
        return 'default'
      default:
        addMessage('system', `Unknown node type "${node.type}"`)
        return '__end__'
    }
  }

  function execMessage(node: ChatNode): string {
    const text = interpolate(stringField(node, 'message', 'text'), state.variables)
    if (text) {
      addMessage('bot', text, { stepName: node.id })
    }
    return 'default'
  }

  function execButtons(node: ChatNode): string {
    const vars = state.variables
    const body = interpolate(stringField(node, 'body', 'message', 'text') || node.label, vars)
    const isList = node.config?.mode === 'list'
    const isCarousel = node.config?.mode === 'carousel'
    if (isCarousel) {
      const cards = resolveNodeCarousel(node, vars)
      addMessage('bot', body, {
        stepName: node.id,
        interactive: 'carousel',
        cards,
        buttons: cards.flatMap((card) => card.buttons),
      })
      state.status = 'waiting_input'
      return '__yield__'
    }
    const buttons = resolveNodeButtons(node, vars)
    const headerImage = isList ? '' : replyHeaderImage(node, vars)
    addMessage('bot', body, {
      stepName: node.id,
      buttons,
      interactive: isList ? 'list' : 'buttons',
      header: isList ? interpolate(stringField(node, 'header'), vars) : undefined,
      headerImage: headerImage || undefined,
      footer: isList ? interpolate(stringField(node, 'footer'), vars) : undefined,
      listButton: isList ? (interpolate(stringField(node, 'list_button'), vars) || 'Select') : undefined,
    })
    state.status = 'waiting_input'
    return '__yield__'
  }

  function execEnd(node: ChatNode): string {
    const text = interpolate(stringField(node, 'message'), state.variables)
    if (text) addMessage('bot', text, { stepName: node.id })
    return '__end__'
  }

  function execPrompt(node: ChatNode): string {
    const body = interpolate(stringField(node, 'body', 'message', 'text'), state.variables)
    if (body) addMessage('bot', body, { stepName: node.id, inputType: 'text' })
    state.status = 'waiting_input'
    return '__yield__'
  }

  function execTransfer(node: ChatNode): string {
    const body = interpolate(stringField(node, 'body', 'message', 'text'), state.variables)
    if (body) addMessage('bot', body, { stepName: node.id })
    const teamID = stringField(node, 'team_id') || '_general'
    const label = teamID === '_general' ? 'General Queue' : teamID
    addMessage('system', `Conversation transferred to ${label}`)
    log('flow_complete', node.id, { reason: 'transfer' })
    state.status = 'completed'
    return '__yield__'
  }

  function execCondition(node: ChatNode): string {
    const expression = stringField(node, 'expression')
    if (!expression) return 'false'
    const result = evalCondition(expression, state.variables)
    log('condition_eval', node.id, { expression, result })
    return result ? 'true' : 'false'
  }

  function execSetVariable(node: ChatNode): string {
    for (const row of readAssignments(node.config?.set)) {
      if (!row.name) continue
      const resolved = resolvePreviewAssignment(row, state.variables)
      if (!resolved.ok) continue
      applyPreviewAssignment(row.name, row.append, resolved.value)
    }
    return 'default'
  }

  function applyPreviewAssignment(name: string, append: boolean, value: unknown): void {
    if (!append) {
      setVariable(name, value)
      return
    }
    const current = state.variables[name]
    if (current == null) {
      setVariable(name, [value])
      return
    }
    if (!Array.isArray(current)) return
    setVariable(name, [...current, value])
  }

  function execTiming(node: ChatNode): string {
    const schedule = (node.config?.schedule as any[] | undefined) || []
    const now = new Date()
    const dayName = ['sunday', 'monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday'][now.getDay()]
    for (const entry of schedule) {
      if (!entry || typeof entry !== 'object') continue
      if (String(entry.day).toLowerCase() !== dayName) continue
      if (!entry.enabled) return 'out_of_hours'
      const nowMinutes = now.getHours() * 60 + now.getMinutes()
      const [sh, sm] = String(entry.start_time || '00:00').split(':').map(Number)
      const [eh, em] = String(entry.end_time || '23:59').split(':').map(Number)
      const startMin = (sh || 0) * 60 + (sm || 0)
      const endMin = (eh || 23) * 60 + (em || 59)
      return nowMinutes >= startMin && nowMinutes < endMin ? 'in_hours' : 'out_of_hours'
    }
    return 'out_of_hours'
  }

  async function execApiCall(node: ChatNode): Promise<string> {
    log('api_call', node.id, {
      url: node.config?.url,
      method: node.config?.method,
      operation: node.config?.operation,
    })
    addMessage('system', `Calling API: ${node.config?.method || 'GET'} ${node.config?.url || ''}`)

    const fakeStep: any = {
      step_name: node.id,
      message: stringField(node, 'message_template') || '',
      api_config: {
        url: stringField(node, 'url'),
        method: stringField(node, 'method') || 'GET',
        headers: (node.config?.headers as Record<string, string>) || {},
        body: stringField(node, 'body'),
        response_mapping: (node.config?.response_mapping as Record<string, string>) || {},
        fallback_message: stringField(node, 'fallback_message'),
      },
    }

    const result = await apiMocker.executeMockedApiCall(fakeStep, state.variables)
    if (result.success && result.data) {
      const extracted = apiMocker.extractVariablesFromResponse(
        result.data,
        fakeStep.api_config.response_mapping || {},
      )
      for (const [k, v] of Object.entries(extracted)) setVariable(k, v)
      addMessage('debug', `API Response (${result.duration}ms): ${JSON.stringify(result.data)}`)
      const template = stringField(node, 'message_template')
      if (template) {
        const rendered = interpolate(template, { ...state.variables, ...extracted })
        if (rendered) addMessage('bot', rendered, { stepName: node.id, isApiMessage: true })
      }
      return 'http:2xx'
    }
    addMessage('debug', `API Error: ${result.error || 'request failed'}`)
    const fb = stringField(node, 'fallback_message')
    if (fb) addMessage('bot', fb, { stepName: node.id, isApiMessage: true })
    return 'http:non2xx'
  }

  async function execTiqrStoreApi(node: ChatNode): Promise<string> {
    const operation = stringField(node, 'operation') || 'list_products'
    const apiType = stringField(node, 'api_type') || 'mcp'
    addMessage('system', `Calling TiQR Store API (${apiType}): ${operation}`)
    return execApiCall({
      ...node,
      config: {
        ...node.config,
        url: `tiqr://${apiType}/${operation}`,
        method: 'POST',
      },
    })
  }

  function execWhatsAppFlow(node: ChatNode): string {
    const body = interpolate(stringField(node, 'body', 'message', 'text'), state.variables)
    if (body) {
      addMessage('bot', body, {
        stepName: node.id,
        inputConfig: {
          flow_id: stringField(node, 'flow_id'),
          flow_cta: stringField(node, 'cta'),
          flow_header: stringField(node, 'header'),
        },
      })
    }
    state.status = 'waiting_input'
    return '__yield__'
  }

  // ---- Inputs from the UI ------------------------------------------------

  function applyPreviewButtonSelection(node: ChatNode, btn: ButtonConfig) {
    const storeAs = stringField(node, 'store_as')
    if (storeAs) setVariable(storeAs, btn.title)
    const mapping = node.config?.selection_mapping
    if (!mapping || typeof mapping !== 'object') return
    const selected: Record<string, any> = { ...(btn.source || {}) }
    if (btn.body && (selected.body == null || selected.body === '')) selected.body = btn.body
    if (btn.media_url && (selected.media_url == null || selected.media_url === '')) selected.media_url = btn.media_url
    selected.id = btn.id
    selected.title = btn.title
    if (btn.description) selected.description = btn.description
    for (const [variable, field] of Object.entries(mapping as Record<string, unknown>)) {
      const name = variable.trim()
      const key = typeof field === 'string' ? field.trim() : ''
      if (!name || !key) continue
      const value = selected[key]
      if (value == null || value === '') continue
      setVariable(name, previewMappedValue(value))
    }
  }

  async function processUserInput(input: UserInput): Promise<void> {
    if (state.status !== 'waiting_input' || !state.currentStepName) return
    const node = nodeById(state.currentStepName)
    if (!node) return

    if (typeof input === 'string') {
      addMessage('user', input)
      if (node.type === 'prompt') {
        const regex = stringField(node, 'validation_regex')
        if (regex) {
          try {
            if (!new RegExp(regex).test(input)) {
              const max = (node.config?.max_retries as number) || 3
              state.currentRetryCount++
              if (state.currentRetryCount < max) {
                addMessage('bot', stringField(node, 'validation_error') || 'Invalid input. Please try again.', {
                  isValidationError: true,
                })
                return
              }
              state.currentRetryCount = 0
              await advance(node, 'max_retries')
              return
            }
          } catch {
            // bad regex: skip validation
          }
        }
        const storeAs = stringField(node, 'store_as')
        if (storeAs) setVariable(storeAs, input)
        state.currentRetryCount = 0
        await advance(node, 'default')
        return
      }
      // A typed reply while buttons, a list, or a carousel is showing stays
      // on this step and shows the choices again, matching WhatsApp.
      if (node.type === 'buttons') {
        execButtons(node)
        return
      }
      state.status = 'waiting_input'
    } else {
      const btn = input
      addMessage('user', btn.title)
      log('branch', node.id, { buttonId: btn.id })
      applyPreviewButtonSelection(node, btn)
      await advance(node, `button:${btn.id}`)
    }
  }

  async function processWhatsAppFlowCompletion(data: Record<string, any>): Promise<void> {
    if (state.status !== 'waiting_input' || !state.currentStepName) return
    const node = nodeById(state.currentStepName)
    if (!node || node.type !== 'whatsapp_flow') return
    addMessage('user', 'Form completed')
    addMessage('debug', `Form data: ${JSON.stringify(data)}`)
    for (const [k, v] of Object.entries(data)) setVariable(k, v)
    await advance(node, 'default')
  }

  async function advance(node: ChatNode, outcome: string): Promise<void> {
    const next = resolveEdge(node.id, outcome)
    log('step_exit', node.id, { outcome, next })
    if (!next) {
      complete()
      return
    }
    state.status = 'running'
    await delay(150)
    await runFrom(next)
  }

  function complete(): void {
    if (flowData.value.completion_message) {
      addMessage('bot', flowData.value.completion_message)
    }
    addMessage('system', 'Flow completed')
    log('flow_complete', state.currentStepName || undefined, { reason: 'end' })
    state.status = 'completed'
  }

  function pauseSimulation(): void {
    if (state.status === 'running' || state.status === 'waiting_input') {
      state.status = 'paused'
    }
  }

  function resumeSimulation(): void {
    if (state.status === 'paused') state.status = 'running'
  }

  function resetSimulation(): void {
    state.status = 'idle'
    state.currentStepIndex = null
    state.currentStepName = null
    state.variables = {}
    state.messages = []
    state.executionLog = []
    state.currentRetryCount = 0
    state.errorMessage = undefined
    snapshots.value = []
  }

  function undo(): boolean {
    const snap = snapshots.value.pop()
    if (!snap) return false
    const restored = JSON.parse(snap)
    state.status = 'paused'
    state.currentStepIndex = restored.currentStepIndex
    state.currentStepName = restored.currentStepName
    state.variables = restored.variables
    state.messages = (restored.messages || []).map((m: any) => ({
      ...m,
      timestamp: new Date(m.timestamp),
    }))
    state.executionLog = (restored.executionLog || []).map((e: any) => ({
      ...e,
      timestamp: new Date(e.timestamp),
    }))
    state.currentRetryCount = restored.currentRetryCount || 0
    state.errorMessage = undefined
    return true
  }

  async function stepForward(): Promise<void> {
    if (state.status !== 'paused' || !state.currentStepName) return
    state.status = 'running'
    await runFrom(state.currentStepName)
  }

  async function goToStep(nodeId: string): Promise<void> {
    if (!nodeById(nodeId)) return
    state.currentStepName = nodeId
    state.status = 'running'
    await runFrom(nodeId)
  }

  return {
    state,
    currentStep,
    currentNode,
    isWaitingForInput,
    expectedInputType,
    canUndo,
    startSimulation,
    pauseSimulation,
    resumeSimulation,
    resetSimulation,
    processUserInput,
    processWhatsAppFlowCompletion,
    undo,
    stepForward,
    goToStep,
    setVariable,
    apiMocker,
  }
}

// adaptNodeAsStep surfaces enough FlowStep-shaped fields that the existing
// InteractivePreview template bindings (currentStep.message_type, etc.)
// keep working without changes. Read-only — mutations bypass this adapter.
function adaptNodeAsStep(node: ChatNode): Record<string, any> {
  const cfg = node.config || {}
  const messageType = nodeTypeToMessageType(node.type)
  const out: Record<string, any> = {
    step_name: node.id,
    message_type: messageType,
    message: stringFromConfig(cfg, 'message', 'body', 'text'),
    input_type: messageType === 'prompt' ? 'text' : 'none',
    buttons: (cfg.buttons as any[]) || [],
    input_config: {
      flow_cta: stringFromConfig(cfg, 'cta'),
      flow_id: stringFromConfig(cfg, 'flow_id'),
      flow_header: stringFromConfig(cfg, 'header'),
    },
    transfer_config: {
      team_id: stringFromConfig(cfg, 'team_id'),
      notes: stringFromConfig(cfg, 'notes'),
    },
  }
  return out
}

function nodeTypeToMessageType(t: string): string {
  switch (t) {
    case 'message':
      return 'text'
    case 'buttons':
      return 'buttons'
    case 'api_call':
      return 'api_fetch'
    case 'tiqr_store_api':
      return 'api_fetch'
    case 'whatsapp_flow':
      return 'whatsapp_flow'
    case 'transfer':
      return 'transfer'
    case 'end':
      return 'end'
    case 'prompt':
      return 'prompt'
    default:
      return t
  }
}

function stringField(node: ChatNode, ...keys: string[]): string {
  return stringFromConfig(node.config || {}, ...keys)
}

function stringFromConfig(cfg: Record<string, any>, ...keys: string[]): string {
  for (const k of keys) {
    if (typeof cfg[k] === 'string' && cfg[k] !== '') return cfg[k]
  }
  return ''
}

function carouselActionTitle(template: string, item: Record<string, any>, vars: Record<string, any>): string {
  const text = template.trim()
  if (!text) return ''
  return interpolate(text, { ...vars, ...item }).trim()
}

function itemDisplayText(template: string, item: Record<string, any>, vars: Record<string, any>): string {
  const text = template.trim()
  if (!text) return ''
  if (text.includes('{{')) return carouselActionTitle(text, item, vars)
  if (Object.prototype.hasOwnProperty.call(item, text)) return previewField(item, text)
  return text
}

function itemTextOrColumn(template: string, column: string, item: Record<string, any>, vars: Record<string, any>): string {
  if (!template.trim()) return previewField(item, column)
  return itemDisplayText(template, item, vars)
}

function previewMappedValue(value: unknown): unknown {
  if (Array.isArray(value) || (typeof value === 'object' && value !== null)) return value
  return typeof value === 'string' ? value : String(value)
}

function previewField(obj: Record<string, any>, key: string): string {
  if (!key) return ''
  const value = obj[key]
  if (value == null) return ''
  return String(value).trim()
}

function previewPath(obj: Record<string, any>, path: string): string {
  const value = lookupPath(obj, path)
  if (value == null || typeof value === 'object') return ''
  return String(value).trim()
}

function resolveNodeCarousel(node: ChatNode, vars: Record<string, any>): PreviewCarouselCard[] {
  const cfg = node.config || {}
  const action = cfg.card_action === 'url' ? 'url' : 'reply'
  const cards: PreviewCarouselCard[] = []

  const pushCard = (card: PreviewCarouselCard) => {
    if (cards.length < 10 && card.mediaUrl && card.buttons.length > 0) cards.push(card)
  }

  if (cfg.source === 'dynamic') {
    const key = String(cfg.items_var || '').trim().replace(/^\{\{/, '').replace(/\}\}$/, '').trim()
    const raw = key ? vars[key] : undefined
    if (!Array.isArray(raw)) return []
    const mediaField = stringFromConfig(cfg, 'media_field') || 'image'
    const bodyField = stringFromConfig(cfg, 'body_field')
    const titleTemplate = stringFromConfig(cfg, 'title_field')
    const idField = stringFromConfig(cfg, 'id_field') || 'id'
    const titleTemplate2 = stringFromConfig(cfg, 'title_field_2')
    const idField2 = stringFromConfig(cfg, 'id_field_2')
    const urlField = stringFromConfig(cfg, 'url_field')
    const buttonTitleTemplate = stringFromConfig(cfg, 'button_title')
    const mediaType = cfg.media_type === 'video' ? 'video' : 'image'
    const fallbackMedia = interpolate(stringFromConfig(cfg, 'fallback_media_url'), vars)
    raw.forEach((item, index) => {
      if (!item || typeof item !== 'object') return
      const obj = item as Record<string, any>
      const mediaUrl = previewPath(obj, mediaField) || fallbackMedia
      const body = bodyField ? itemDisplayText(bodyField, obj, vars) : ''
      const buttons = carouselButtons(action, {
        title: action === 'url'
          ? (buttonTitleTemplate.trim()
            ? itemDisplayText(buttonTitleTemplate, obj, vars)
            : itemTextOrColumn(titleTemplate, 'title', obj, vars))
          : itemDisplayText(titleTemplate, obj, vars),
        id: previewField(obj, idField) || `card_${index + 1}`,
        url: itemTextOrColumn(urlField, 'url', obj, vars),
        title2: action === 'url' || !titleTemplate2 ? '' : itemDisplayText(titleTemplate2, obj, vars),
        id2: previewField(obj, idField2) || `card_${index + 1}_b`,
        requireSecond: action !== 'url' && Boolean(titleTemplate2),
        body,
        mediaUrl,
        mediaType,
        source: obj,
      })
      if (!buttons) return
      pushCard({ mediaType, mediaUrl, body: body || undefined, buttons })
    })
    return cards
  }

  const buttons = (cfg.buttons as Record<string, any>[] | undefined) || []
  const fallbackMedia = interpolate(stringFromConfig(cfg, 'fallback_media_url'), vars)
  buttons.forEach((card, index) => {
    const mediaType = card.media_type === 'video' ? 'video' : 'image'
    const mediaUrl = interpolate(String(card.media_url || ''), vars) || fallbackMedia
    const body = interpolate(String(card.body || ''), vars)
    const built = carouselButtons(action, {
      title: interpolate(String(card.title || ''), vars),
      id: interpolate(String(card.id || ''), vars) || `card_${index + 1}`,
      url: interpolate(String(card.url || ''), vars),
      title2: interpolate(String(card.title_2 || ''), vars),
      id2: interpolate(String(card.id_2 || ''), vars) || `card_${index + 1}_b`,
      requireSecond: false,
      body,
      mediaUrl,
      mediaType,
    })
    if (!built) return
    pushCard({ mediaType, mediaUrl, body: body || undefined, buttons: built })
  })
  return cards
}

function carouselButtons(action: 'url' | 'reply', card: {
  title: string
  id: string
  url: string
  title2: string
  id2: string
  requireSecond: boolean
  body: string
  mediaUrl: string
  mediaType: 'image' | 'video'
  source?: Record<string, any>
}): ButtonConfig[] | null {
  if (!card.mediaUrl || !card.title) return null
  const shared = {
    body: card.body || undefined,
    media_url: card.mediaUrl,
    media_type: card.mediaType,
    source: card.source,
  }
  if (action === 'url') {
    if (!card.url) return null
    return [{ id: card.id, title: card.title, type: 'url', url: card.url, ...shared }]
  }
  const buttons: ButtonConfig[] = [{ id: card.id, title: card.title, type: 'reply', ...shared }]
  if (card.title2) {
    buttons.push({ id: card.id2, title: card.title2, type: 'reply', ...shared })
  } else if (card.requireSecond) {
    return null
  }
  return buttons
}

function replyHeaderImage(node: ChatNode, vars: Record<string, any>): string {
  const cfg = node.config || {}
  const rendered = interpolate(String(cfg.header_image || ''), vars).trim()
  if (rendered) return rendered
  return interpolate(String(cfg.fallback_media_url || ''), vars).trim()
}

// resolveNodeButtons mirrors the backend buttonsForNode mapping so the
// preview shows static cards or rows built from a simulation variable.
function resolveNodeButtons(node: ChatNode, vars: Record<string, any>): ButtonConfig[] {
  const cfg = node.config || {}
  const mode = cfg.mode === 'list' ? 'list' : 'reply'
  if (cfg.source !== 'dynamic') {
    const buttons = ((cfg.buttons as ButtonConfig[] | undefined) || []).map((btn) => ({
      ...btn,
      title: interpolate(btn.title || '', vars),
      description: btn.description ? interpolate(btn.description, vars) : btn.description,
      url: btn.url ? interpolate(btn.url, vars) : btn.url,
      phone_number: btn.phone_number ? interpolate(btn.phone_number, vars) : btn.phone_number,
    }))
    return mode === 'list' ? buttons.slice(0, 10) : buttons
  }

  const key = String(cfg.items_var || '').trim().replace(/^\{\{/, '').replace(/\}\}$/, '').trim()
  const raw = key ? vars[key] : undefined
  if (!Array.isArray(raw)) return []

  let kind = 'reply'
  if (mode === 'list') kind = 'list'
  else if (cfg.dynamic_type === 'url' || cfg.dynamic_type === 'phone' || cfg.dynamic_type === 'reply') {
    kind = cfg.dynamic_type
  }
  const titleField = stringFromConfig(cfg, 'title_field')
  const idField = stringFromConfig(cfg, 'id_field') || 'id'
  const descField = stringFromConfig(cfg, 'description_field')
  const urlField = stringFromConfig(cfg, 'url_field')
  const phoneField = stringFromConfig(cfg, 'phone_field')
  const limit = kind === 'url' || kind === 'phone' ? 2 : 10

  const out: ButtonConfig[] = []
  raw.forEach((item, index) => {
    if (out.length >= limit || !item || typeof item !== 'object') return
    const obj = item as Record<string, any>
    const title = itemTextOrColumn(titleField, 'title', obj, vars)
    if (!title) return
    if (kind === 'url') {
      out.push({ id: `url_${index + 1}`, title, type: 'url', url: itemTextOrColumn(urlField, 'url', obj, vars), source: obj })
      return
    }
    if (kind === 'phone') {
      out.push({ id: `phone_${index + 1}`, title, type: 'phone', phone_number: itemTextOrColumn(phoneField, 'phone_number', obj, vars), source: obj })
      return
    }
    const id = previewField(obj, idField) || `btn_${index + 1}`
    const description = kind === 'list' && descField ? itemDisplayText(descField, obj, vars) : undefined
    out.push({ id, title, type: 'reply', description: description || undefined, source: obj })
  })
  if (kind === 'reply' && out.length < limit) {
    const extraTitle = stringFromConfig(cfg, 'title_field_2').trim()
    if (extraTitle && !extraTitle.includes('{{')) {
      let extraID = stringFromConfig(cfg, 'id_field_2').trim()
      if (!extraID || extraID.includes('{{') || extraID.includes('.') || extraID.includes('[')) extraID = 'btn_extra'
      out.push({ id: extraID, title: extraTitle, type: 'reply' })
    }
  }
  return out
}

// evalExpression runs an expr-lang-style expression and returns the raw
// value. Unknown names resolve to undefined. A compile or runtime error
// returns ok: false so the caller can skip that assignment.
// evalCondition coerces the same result to bool.
function evalExpression(expression: string, vars: Record<string, any>): { ok: boolean; value: unknown } {
  const trimmed = expression.trim()
  if (!trimmed) return { ok: false, value: undefined }
  try {
    const js = translateExprToJs(trimmed)
    // eslint-disable-next-line @typescript-eslint/no-implied-eval, no-new-func
    const fn = new Function('vars', `
      const len = (value) => {
        if (typeof value === 'string' || Array.isArray(value)) return value.length
        if (value && typeof value === 'object') return Object.keys(value).length
        return 0
      }
      const __in = (collection, value) => {
        if (typeof collection === 'string') return collection.includes(String(value ?? ''))
        if (Array.isArray(collection)) return collection.includes(value)
        if (collection && typeof collection === 'object') return Object.prototype.hasOwnProperty.call(collection, String(value))
        return false
      }
      const env = new Proxy(vars, {
        has(_target, prop) {
          return prop !== 'len' && prop !== '__in'
        },
        get(target, prop) {
          if (prop === Symbol.unscopables) return undefined
          if (typeof prop === 'string' && Object.prototype.hasOwnProperty.call(target, prop)) return target[prop]
          return undefined
        },
      })
      try {
        with (env) { return { ok: true, value: (${js}) } }
      } catch (_) {
        return { ok: false, value: undefined }
      }
    `)
    return fn(vars) as { ok: boolean; value: unknown }
  } catch {
    return { ok: false, value: undefined }
  }
}

function evalCondition(expression: string, vars: Record<string, any>): boolean {
  const result = evalExpression(expression, vars)
  if (!result.ok) return false
  return coerceExprBool(result.value)
}

function coerceExprBool(value: unknown): boolean {
  if (typeof value === 'boolean') return value
  if (value == null) return false
  if (typeof value === 'string') return value !== '' && value.toLowerCase() !== 'false'
  if (typeof value === 'number') return value !== 0 && !Number.isNaN(value)
  return true
}

type PreviewAssignment = {
  name: string
  isExpr: boolean
  isJSON: boolean
  append: boolean
  expr: string
  raw: unknown
}

function readAssignments(set: unknown): PreviewAssignment[] {
  if (Array.isArray(set)) {
    return set.flatMap((item) => {
      if (!item || typeof item !== 'object') return []
      return [previewAssignmentFromRow(item as Record<string, unknown>)]
    })
  }
  if (set && typeof set === 'object') {
    return Object.entries(set as Record<string, unknown>).map(([name, value]) => previewAssignment(name, value))
  }
  return []
}

function previewAssignmentFromRow(row: Record<string, unknown>): PreviewAssignment {
  const name = typeof row.name === 'string' ? row.name : ''
  const append = row.op === 'append'
  if (row.value_type === 'json') {
    return {
      name,
      isExpr: false,
      isJSON: true,
      append,
      expr: typeof row.value === 'string' ? row.value : '',
      raw: row.value,
    }
  }
  return { ...previewAssignment(name, row.value), append }
}

function previewAssignment(name: string, raw: unknown): PreviewAssignment {
  if (typeof raw === 'string') return { name, isExpr: true, isJSON: false, append: false, expr: raw, raw }
  return { name, isExpr: false, isJSON: false, append: false, expr: '', raw }
}

const EXACT_VAR_RE = /^\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*|\[\d+\])*)\s*\}\}$/

function resolvePreviewAssignment(row: PreviewAssignment, vars: Record<string, any>): { ok: boolean; value: unknown } {
  if (row.isJSON) return resolveJSONAssignment(row.expr, vars)
  if (!row.isExpr) return { ok: true, value: row.raw }
  return evalExpression(row.expr, vars)
}

function resolveJSONAssignment(text: string, vars: Record<string, any>): { ok: boolean; value: unknown } {
  try {
    return { ok: true, value: resolveJSONTemplates(JSON.parse(text), vars) }
  } catch {
    return { ok: false, value: undefined }
  }
}

function resolveJSONTemplates(value: unknown, vars: Record<string, any>): unknown {
  if (typeof value === 'string') {
    const match = value.match(EXACT_VAR_RE)
    if (match) {
      const looked = lookupPath(vars, match[1])
      return looked === undefined ? null : looked
    }
    return interpolate(value, vars)
  }
  if (Array.isArray(value)) return value.map((item) => resolveJSONTemplates(item, vars))
  if (value && typeof value === 'object') {
    const out: Record<string, unknown> = {}
    for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
      out[key] = resolveJSONTemplates(child, vars)
    }
    return out
  }
  return value
}

function translateExprToJs(expression: string): string {
  let js = expression
  js = replaceInfix(js, 'startsWith', (left, right) => `(${left}).startsWith(${right})`)
  js = replaceInfix(js, 'contains', (left, right) => `(${left}).includes(${right})`)
  js = replaceInfix(js, 'in', (left, right) => `__in(${right}, ${left})`)
  return js
    .replace(/\band\b/gi, '&&')
    .replace(/\bor\b/gi, '||')
    .replace(/\bnot\b/gi, '!')
}

function replaceInfix(
  source: string,
  keyword: string,
  wrap: (left: string, right: string) => string,
): string {
  let result = source
  for (;;) {
    const match = findInfixKeyword(result, keyword)
    if (!match) return result
    const left = readOperandBackward(result, match.start)
    const right = readOperandForward(result, match.end)
    if (!left || !right) return result
    result = result.slice(0, left.start) + wrap(left.text, right.text) + result.slice(right.end)
  }
}

function findInfixKeyword(source: string, keyword: string): { start: number; end: number } | null {
  const lower = keyword.toLowerCase()
  let quote: string | null = null
  for (let i = 0; i < source.length; i++) {
    const ch = source[i]
    if (quote) {
      if (ch === '\\') { i++; continue }
      if (ch === quote) quote = null
      continue
    }
    if (ch === '"' || ch === "'") { quote = ch; continue }
    if (source.slice(i, i + keyword.length).toLowerCase() !== lower) continue
    const before = i === 0 ? ' ' : source[i - 1]
    const after = source[i + keyword.length] ?? ' '
    const isBoundary = (c: string) => !/[A-Za-z0-9_]/.test(c)
    if (!isBoundary(before) || !isBoundary(after) || before === '.') continue
    return { start: i, end: i + keyword.length }
  }
  return null
}

function readOperandBackward(source: string, endExclusive: number): { text: string; start: number } | null {
  let i = endExclusive - 1
  while (i >= 0 && /\s/.test(source[i])) i--
  if (i < 0) return null
  const operandEnd = i + 1
  while (i >= 0) {
    const ch = source[i]
    if (ch === ')' || ch === ']') {
      const open = ch === ')' ? '(' : '['
      let depth = 1
      i--
      while (i >= 0 && depth > 0) {
        const current = source[i]
        if (current === '"' || current === "'") {
          const q = current
          i--
          while (i >= 0 && source[i] !== q) {
            if (source[i] === '\\') i--
            i--
          }
          i--
          continue
        }
        if (current === ch) depth++
        else if (current === open) depth--
        i--
      }
      continue
    }
    if (ch === '"' || ch === "'") {
      i--
      while (i >= 0 && source[i] !== ch) {
        if (source[i] === '\\') i--
        i--
      }
      i--
      continue
    }
    if (/[A-Za-z0-9_.]/.test(ch)) {
      while (i >= 0 && /[A-Za-z0-9_.]/.test(source[i])) i--
      continue
    }
    break
  }
  const start = i + 1
  const text = source.slice(start, operandEnd).trim()
  if (!text) return null
  return { text, start }
}

function readOperandForward(source: string, start: number): { text: string; end: number } | null {
  let i = start
  while (i < source.length && /\s/.test(source[i])) i++
  if (i >= source.length) return null
  const begin = i
  const ch = source[i]
  if (ch === '"' || ch === "'") {
    i++
    while (i < source.length && source[i] !== ch) {
      if (source[i] === '\\') i++
      i++
    }
    return { text: source.slice(begin, i + 1), end: i + 1 }
  }
  if (ch === '(' || ch === '[') {
    const close = ch === '(' ? ')' : ']'
    i = skipGroup(source, i, ch, close)
    i = skipMemberTail(source, i)
    return { text: source.slice(begin, i), end: i }
  }
  if (!/[A-Za-z0-9_]/.test(ch)) return null
  while (i < source.length && /[A-Za-z0-9_.]/.test(source[i])) i++
  i = skipMemberTail(source, i)
  return { text: source.slice(begin, i), end: i }
}

function skipGroup(source: string, start: number, open: string, close: string): number {
  let i = start
  let depth = 0
  while (i < source.length) {
    const ch = source[i]
    if (ch === '"' || ch === "'") {
      i++
      while (i < source.length && source[i] !== ch) {
        if (source[i] === '\\') i++
        i++
      }
      i++
      continue
    }
    if (ch === open) depth++
    else if (ch === close) {
      depth--
      i++
      if (depth === 0) return i
      continue
    }
    i++
  }
  return i
}

function skipMemberTail(source: string, start: number): number {
  let i = start
  while (i < source.length && (source[i] === '.' || source[i] === '[')) {
    if (source[i] === '.') {
      i++
      while (i < source.length && /[A-Za-z0-9_]/.test(source[i])) i++
      continue
    }
    i = skipGroup(source, i, '[', ']')
  }
  return i
}
