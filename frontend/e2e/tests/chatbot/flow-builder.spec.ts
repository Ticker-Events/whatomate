import { test, expect } from '@playwright/test'
import { loginAsAdmin } from '../../helpers'
import { ChatbotFlowBuilderPage } from '../../pages'

// After the editor refactor the chat flow builder mirrors the IVR editor:
// no left-side steps list, no step_order, no message-type switching after
// the fact. The palette toolbar adds typed nodes; the right panel reflects
// whichever node is selected.

test.describe('Chatbot Flow Builder - Palette', () => {
  let builder: ChatbotFlowBuilderPage

  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page)
    builder = new ChatbotFlowBuilderPage(page)
    await builder.gotoNew()
  })

  test('palette shows the action-node tiles', async () => {
    await expect(builder.paletteToolbar).toBeVisible()
    for (const label of ['Text', 'Buttons', 'API', 'TiQR Store', 'Transfer', 'Condition', 'Timing', 'End']) {
      await expect(builder.paletteToolbar.getByRole('button', { name: label, exact: true })).toBeVisible()
    }
  })

  test('"prompt" and "webhook" are not in the palette', async () => {
    // Authors get prompt behaviour by setting an Expected response on a
    // Text node — no standalone tile.
    await expect(builder.paletteToolbar.getByRole('button', { name: 'Prompt', exact: true })).toHaveCount(0)
    await expect(builder.paletteToolbar.getByRole('button', { name: 'Webhook', exact: true })).toHaveCount(0)
  })
})

test.describe('Chatbot Flow Builder - Text node', () => {
  let builder: ChatbotFlowBuilderPage

  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page)
    builder = new ChatbotFlowBuilderPage(page)
    await builder.gotoNew()
    await builder.addNode('Text')
  })

  test('shows the message textarea in the right panel', async () => {
    await expect(builder.messageTextarea).toBeVisible()
  })

  test('Expected response defaults to None (fire-and-forget)', async () => {
    await expect(builder.page.getByText('Expected response')).toBeVisible()
  })
})

test.describe('Chatbot Flow Builder - Buttons node', () => {
  let builder: ChatbotFlowBuilderPage

  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page)
    builder = new ChatbotFlowBuilderPage(page)
    await builder.gotoNew()
    await builder.addNode('Buttons')
  })

  test('shows the button options section', async () => {
    await expect(builder.buttonOptionsLabel).toBeVisible()
  })

  test('shows Reply, URL and Phone add-buttons', async () => {
    await expect(builder.addReplyButton).toBeVisible()
    await expect(builder.addUrlButton).toBeVisible()
    await expect(builder.addPhoneButton).toBeVisible()
  })

  test('shows a body textarea alongside buttons config', async () => {
    await expect(builder.bodyTextarea).toBeVisible()
    await expect(builder.buttonOptionsLabel).toBeVisible()
  })

  test('adds a reply button', async () => {
    await builder.addReplyButton.click()
    await expect(builder.getButtonTitleInput(0)).toBeVisible()
    await expect(builder.buttonOptionsLabel).toContainText('1/10')
  })

  test('adds a URL button with /2 count', async () => {
    await builder.addUrlButton.click()
    await expect(builder.getButtonTitleInput(0)).toBeVisible()
    await expect(builder.page.getByPlaceholder(/https:\/\/example.com/i)).toBeVisible()
    await expect(builder.buttonOptionsLabel).toContainText('1/2')
  })

  test('adds a phone button', async () => {
    await builder.addPhoneButton.click()
    await expect(builder.getButtonTitleInput(0)).toBeVisible()
    await expect(builder.page.getByPlaceholder(/\+1234567890/)).toBeVisible()
    await expect(builder.buttonOptionsLabel).toContainText('1/2')
  })

  test('counts multiple reply buttons', async () => {
    await builder.addReplyButton.click()
    await builder.addReplyButton.click()
    await builder.addReplyButton.click()
    await expect(builder.buttonOptionsLabel).toContainText('3/10')
  })

  test('counts multiple CTA buttons', async () => {
    await builder.addUrlButton.click()
    await builder.addPhoneButton.click()
    await expect(builder.buttonOptionsLabel).toContainText('2/2')
  })

  test('disables CTA buttons when a reply button exists', async () => {
    await builder.addReplyButton.click()
    await expect(builder.addUrlButton).toBeDisabled()
    await expect(builder.addPhoneButton).toBeDisabled()
    await expect(builder.addReplyButton).toBeEnabled()
  })

  test('disables reply button when a CTA button exists', async () => {
    await builder.addUrlButton.click()
    await expect(builder.addReplyButton).toBeDisabled()
    await expect(builder.addPhoneButton).toBeEnabled()
  })

  test('enforces max 2 CTA buttons', async () => {
    await builder.addUrlButton.click()
    await builder.addPhoneButton.click()
    await expect(builder.addUrlButton).toBeDisabled()
    await expect(builder.addPhoneButton).toBeDisabled()
  })

  test('removes a button', async () => {
    await builder.addReplyButton.click()
    await expect(builder.buttonOptionsLabel).toContainText('1/10')
    await builder.getButtonDeleteButton(0).click()
    await expect(builder.buttonOptionsLabel).toContainText('0/10')
  })
})

test.describe('Chatbot Flow Builder - Other node types', () => {
  let builder: ChatbotFlowBuilderPage

  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page)
    builder = new ChatbotFlowBuilderPage(page)
    await builder.gotoNew()
  })

  test('API node exposes method + URL fields', async () => {
    await builder.addNode('API')
    await expect(builder.page.getByText(/^Method$/i).first()).toBeVisible()
    await expect(builder.page.getByText(/^URL$/i).first()).toBeVisible()
  })

  test('TiQR Store API node exposes API type and operation dropdowns', async () => {
    await builder.addNode('TiQR Store')
    await expect(builder.page.getByText(/^API type$/i).first()).toBeVisible()
    await expect(builder.page.getByText(/^Operation$/i).first()).toBeVisible()
    await expect(builder.page.getByText(/Uses Commerce MCP URL/i)).toBeVisible()
    await expect(builder.page.getByText(/^Search$/)).toHaveCount(0)
    await expect(builder.page.getByText(/^Product ID$/)).toHaveCount(0)

    await builder.page.getByRole('combobox').filter({ hasText: /List products/i }).click()
    await builder.page.getByRole('option', { name: 'Search products' }).click()
    await expect(builder.page.getByText(/^Search$/)).toBeVisible()
    await expect(builder.page.getByText(/^Product ID$/)).toHaveCount(0)

    await builder.page.getByRole('combobox').filter({ hasText: /Search products/i }).click()
    await builder.page.getByRole('option', { name: 'Product details' }).click()
    await expect(builder.page.getByText(/^Product ID$/)).toBeVisible()
    await expect(builder.page.getByText(/^Search$/)).toHaveCount(0)

    // REST mode hides MCP-only ops (Check delivery / Lookup order status / Retry payment).
    await builder.page.getByRole('combobox').filter({ hasText: /^MCP$/ }).click()
    await builder.page.getByRole('option', { name: 'REST', exact: true }).click()
    await expect(builder.page.getByText(/Uses Commerce REST Endpoint URL/i)).toBeVisible()
    await builder.page.getByRole('combobox').filter({ hasText: /Product details|List products|Search products/i }).first().click()
    await expect(builder.page.getByRole('option', { name: 'Check delivery' })).toHaveCount(0)
    await expect(builder.page.getByRole('option', { name: 'Lookup order status' })).toHaveCount(0)
    await expect(builder.page.getByRole('option', { name: 'Retry payment' })).toHaveCount(0)
    await expect(builder.page.getByRole('option', { name: 'List products' })).toBeVisible()
  })

  test('Transfer node exposes a team selector', async () => {
    await builder.addNode('Transfer')
    await expect(builder.page.getByText(/^Team$/i).first()).toBeVisible()
  })

  test('Condition node exposes the expression textarea', async () => {
    await builder.addNode('Condition')
    await expect(builder.page.getByText(/^Expression$/i).first()).toBeVisible()
  })
})

test.describe('Chatbot Flow Builder - Preview', () => {
  let builder: ChatbotFlowBuilderPage

  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page)
    builder = new ChatbotFlowBuilderPage(page)
    await builder.gotoNew()
  })

  test('skips the start sentinel and runs the first Text node', async () => {
    const textBody = 'Hello from preview text node'
    await builder.addNode('Text')
    await builder.messageTextarea.fill(textBody)

    await builder.openPreviewAndStart()

    const phone = builder.previewPhoneFrame
    await expect(phone.getByText(/Unknown node type "start"/i)).toHaveCount(0)
    await expect(phone.getByText('Hi! Let me help you with that.')).toBeVisible()
    await expect(phone.getByText(textBody)).toBeVisible()
  })
})
