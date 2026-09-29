<script setup lang="ts">
import { computed } from 'vue'
import type { ChatFlowGraph } from '@/services/api'
import type { FlowData, ButtonConfig } from '@/types/flow-preview'
import { useFlowGraphSimulation } from '@/composables/useFlowGraphSimulation'
import DebugPanel from './DebugPanel.vue'
import ApiMockDialog from './ApiMockDialog.vue'
import PreviewPhone from './PreviewPhone.vue'

const props = defineProps<{
  graph: ChatFlowGraph | null
  flowData: Partial<FlowData>
}>()

const graphRef = computed(() => props.graph)
const flowDataRef = computed(() => props.flowData)

const {
  state,
  currentStep,
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
  apiMocker,
} = useFlowGraphSimulation(graphRef, flowDataRef)

const flowCta = computed(() => currentStep.value?.input_config?.flow_cta || 'Open Form')

// Shim for DebugPanel which expects FlowStep[]-shaped objects with step_name.
const debugSteps = computed(() => (props.graph?.nodes || []).map((n) => ({
  step_name: n.id,
  message_type: n.type,
})))

// Get current node for API mock dialog (node-shaped, but the dialog only
// reads step_name + api_config). We surface an adapter so existing
// ApiMockDialog props keep working without a refactor.
const currentApiStep = computed(() => {
  const mockingId = apiMocker.currentMockStep.value
  if (!mockingId) return null
  const node = props.graph?.nodes.find((n) => n.id === mockingId)
  if (!node) return null
  return {
    step_name: node.id,
    api_config: {
      url: (node.config?.url as string) || '',
      method: (node.config?.method as string) || 'GET',
      headers: (node.config?.headers as Record<string, string>) || {},
      body: (node.config?.body as string) || '',
      response_mapping: (node.config?.response_mapping as Record<string, string>) || {},
      fallback_message: (node.config?.fallback_message as string) || '',
    },
  } as any
})

function handleButtonSelect(button: ButtonConfig) {
  processUserInput(button)
}

function handleTextSubmit(value: string) {
  processUserInput(value)
}

function handleWhatsAppFlowComplete() {
  processWhatsAppFlowCompletion({})
}

function handleStart() {
  startSimulation()
}

function handlePause() {
  pauseSimulation()
}

function handleResume() {
  resumeSimulation()
}

function handleReset() {
  resetSimulation()
  // "Reset" in the debug panel really means restart — users expect a
  // fresh run, not a frozen idle screen.
  startSimulation()
}

function handleStepForward() {
  stepForward()
}

function handleUndo() {
  undo()
}

function handleGoToStep(stepName: string) {
  goToStep(stepName)
}
</script>

<template>
  <div class="flex-1 flex h-full min-h-0">
    <PreviewPhone
      :name="flowData.name || 'Flow Preview'"
      :status="state.status"
      :status-label="state.currentStepName || ''"
      :messages="state.messages"
      :waiting="isWaitingForInput"
      :input-type="expectedInputType"
      :flow-cta="flowCta"
      @select="handleButtonSelect"
      @submit="handleTextSubmit"
      @complete-flow="handleWhatsAppFlowComplete"
    />

    <!-- Debug Panel -->
    <div class="w-64 flex-shrink-0">
      <DebugPanel
        :state="state"
        :steps="debugSteps as any"
        :can-undo="canUndo"
        @start="handleStart"
        @pause="handlePause"
        @resume="handleResume"
        @reset="handleReset"
        @step-forward="handleStepForward"
        @undo="handleUndo"
        @go-to-step="handleGoToStep"
      />
    </div>

    <!-- API Mock Dialog -->
    <ApiMockDialog
      :open="apiMocker.showMockDialog.value"
      :step="currentApiStep || null"
      @update:open="(open) => { if (!open) apiMocker.submitMockConfig(null) }"
      @submit="apiMocker.submitMockConfig"
    />
  </div>
</template>

