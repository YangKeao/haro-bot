import { expect, test, type Page, type Route } from '@playwright/test'

const agent = {
  id: 1,
	provider_id: 1,
	provider_name: 'Codex OAuth',
  name: 'Research Atlas',
  description: 'Synthesizes sources into clear, decision-ready briefs.',
  icon: 'sparkles',
	color: '#2563eb',
	avatar_mode: 'icon',
  instructions: 'Be precise.',
  model: 'gpt-5.6-sol',
	reasoning_effort_override: null,
	context_window_override: null,
	auto_compact_token_limit_override: null,
	resolved_context_window: 200000,
	resolved_auto_compact_token_limit: 160000,
  effective_context_window_percent: 90,
  skill_names: ['web-search'],
  created_at: '2026-08-14T01:00:00Z',
  updated_at: '2026-08-14T01:00:00Z',
}

const provider = {
	id: 1,
	name: 'Codex OAuth',
	base_url: 'https://openai-oauth.example/v1',
	api_key_configured: false,
	prompt_format: 'openai',
	model_count: 1,
	models_fetched_at: '2026-08-14T01:00:00Z',
	catalog_stale: false,
	created_at: '2026-08-14T01:00:00Z',
	updated_at: '2026-08-14T01:00:00Z',
}

const modelCatalog = {
	fetched_at: '2026-08-14T01:00:00Z',
	last_error: '',
	stale: false,
	models: [{
		id: 'gpt-5.6-sol',
		display_name: 'GPT-5.6 Sol',
		description: 'A reasoning model for complex work.',
		context_window: 200000,
		auto_compact_token_limit: 160000,
		default_reasoning_effort: 'medium',
		reasoning_efforts: [{ value: 'low', description: 'Faster' }, { value: 'medium', description: 'Balanced' }, { value: 'high', description: 'Deeper' }],
		input_modalities: ['text', 'image'],
	}],
}

const session = {
  id: 10,
  agent_id: 1,
  title: 'Quarterly market brief',
  created_at: '2026-08-14T01:00:00Z',
  updated_at: '2026-08-14T02:00:00Z',
}

const archivedAgent = { ...agent, id: 2, name: 'Legacy Analyst', archived_at: '2026-08-13T01:00:00Z' }
const archivedSession = { ...session, id: 11, title: 'Archived design review', archived_at: '2026-08-13T01:00:00Z' }
const recentSession = {
  ...session,
  agent: { id: agent.id, name: agent.name, icon: agent.icon, color: agent.color, avatar_mode: agent.avatar_mode },
}

async function mockAPI(page: Page, initiallyAuthenticated = false) {
  let authenticated = initiallyAuthenticated
  let agentArchived = true
  let sessionArchived = true
  await page.route('**/api/v1/**', async (route: Route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    if (path === '/api/v1/auth/session') {
      await route.fulfill(authenticated
        ? { status: 200, contentType: 'application/json', body: JSON.stringify({ authenticated: true }) }
        : { status: 401, contentType: 'application/json', body: JSON.stringify({ error: { code: 'unauthorized', message: 'Authentication required' } }) })
      return
    }
    if (path === '/api/v1/auth/login') {
      authenticated = true
      await route.fulfill({ status: 204 })
      return
    }
    if (path === '/api/v1/agents') {
      const agents = url.searchParams.get('archived') === 'true'
        ? [agent, ...(agentArchived ? [archivedAgent] : [])]
        : [agent, ...(!agentArchived ? [{ ...archivedAgent, archived_at: undefined }] : [])]
      return route.fulfill({ json: { agents } })
    }
    if (path === '/api/v1/agents/2/restore') {
      agentArchived = false
      return route.fulfill({ json: { archived: false } })
    }
    if (path === '/api/v1/agents/1') return route.fulfill({ json: agent })
    if (path === '/api/v1/agents/1/sessions') {
      const sessions = url.searchParams.get('archived') === 'true'
        ? (sessionArchived ? [archivedSession] : [])
        : [session, ...(!sessionArchived ? [{ ...archivedSession, archived_at: undefined }] : [])]
      return route.fulfill({ json: { sessions } })
    }
		if (path === '/api/v1/sessions/recent') return route.fulfill({ json: { sessions: [recentSession] } })
		if (path === '/api/v1/providers') return route.fulfill({ json: { providers: [provider] } })
		if (path === '/api/v1/providers/1') return route.fulfill({ json: provider })
		if (path === '/api/v1/providers/1/models') return route.fulfill({ json: modelCatalog })
		if (path === '/api/v1/providers/1/models/refresh') return route.fulfill({ json: modelCatalog })
		if (path === '/api/v1/integrations/telegram') {
			return route.fulfill({ json: { token_configured: true, agent_id: 1 } })
		}
    if (path === '/api/v1/sessions/11/restore') {
      sessionArchived = false
      return route.fulfill({ json: { archived: false } })
    }
    if (path === '/api/v1/sessions/10') return route.fulfill({ json: { session, status: { State: 'idle' } } })
    if (path === '/api/v1/sessions/11') return route.fulfill({ json: { session: { ...archivedSession, archived_at: undefined }, status: { State: 'idle' } } })
    if (path === '/api/v1/sessions/10/messages') {
      return route.fulfill({ json: { messages: [
        { id: 1, session_id: 10, role: 'user', content: 'Summarize the market signals.', created_at: '2026-08-14T01:00:00Z' },
        { id: 2, session_id: 10, role: 'assistant', content: 'The strongest signal is sustained demand with moderating growth.', created_at: '2026-08-14T01:01:00Z' },
      ] } })
    }
		if (path === '/api/v1/sessions/11/messages') return route.fulfill({ json: { messages: [] } })
		if (path === '/api/v1/skills') return route.fulfill({ json: { skills: [{ name: 'web-search', description: 'Search public sources', version: '1', hash: 'abc' }] } })
    await route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: { code: 'not_found', message: path } }) })
  })
}

test('signs in and opens an agent conversation', async ({ page }) => {
  await mockAPI(page)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible()
  await page.getByLabel('Access token').fill('review-token')
  await page.getByRole('button', { name: 'Open workspace' }).click()

  await expect(page.getByRole('heading', { name: 'Welcome back.' })).toBeVisible()
  await page.getByRole('link', { name: /Quarterly market brief/ }).click()
  await expect(page.getByRole('heading', { name: 'Quarterly market brief' })).toBeVisible()
  await expect(page.getByText('The strongest signal is sustained demand with moderating growth.')).toBeVisible()
})

test('finds and restores archived agents', async ({ page }) => {
  await mockAPI(page, true)
  await page.goto('/')
  await page.getByRole('button', { name: 'Archived' }).click()
  await expect(page.getByText('Legacy Analyst')).toBeVisible()
  await page.getByRole('button', { name: 'Restore' }).click()
  await expect(page.getByRole('link', { name: /Legacy Analyst/ })).toBeVisible()
})

test('keeps agent and session navigation usable on mobile', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await mockAPI(page, true)
  await page.goto('/agents/1/sessions/10')
	const chatBounds = await page.locator('.chat-panel').boundingBox()
	expect(chatBounds?.width).toBeGreaterThanOrEqual(389)

  await page.getByRole('button', { name: 'Open agents' }).click()
  await expect(page.getByRole('complementary').filter({ hasText: 'Agents' })).toBeVisible()
  await page.getByRole('button', { name: 'Close agents' }).click()
  await page.getByRole('button', { name: 'Open conversations' }).click()
  await expect(page.getByText('Conversations', { exact: true })).toBeVisible()
  await expect(page.getByText('Quarterly market brief').first()).toBeVisible()
  await page.getByRole('button', { name: 'Archived conversations' }).click()
  await expect(page.getByText('Archived design review')).toBeVisible()
  await page.getByRole('button', { name: 'Restore Archived design review' }).click()
  await page.waitForURL(/\/agents\/1\/sessions\/11$/)
})

test('uses labeled navigation and persists appearance preferences', async ({ page }) => {
  await mockAPI(page, true)
  await page.goto('/')
  await expect(page.getByRole('link', { name: 'Home', exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: 'New agent' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Guidelines' })).toBeVisible()
  await page.getByRole('button', { name: 'Appearance' }).click()
  await page.getByRole('menuitem', { name: /Dusty rose/ }).click()
  await expect(page.locator('html')).toHaveAttribute('data-accent', 'rose')
  await expect(page.getByRole('button', { name: 'Collapse navigation' })).toHaveCount(0)
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('data-accent', 'rose')
})

test('uses a labeled navigation drawer on mobile', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await mockAPI(page, true)
  await page.goto('/')
  await expect(page.getByRole('button', { name: 'Open navigation' })).toBeVisible()
  await page.getByRole('button', { name: 'Open navigation' }).click()
  await expect(page.getByRole('navigation', { name: 'Mobile navigation' }).getByRole('link', { name: 'Providers' })).toBeVisible()
  await expect(page.getByText('Accent color')).toBeVisible()
  await page.getByRole('button', { name: 'Close navigation' }).click()
})

test('shows every agent setting section on one page', async ({ page }) => {
  await mockAPI(page, true)
  await page.goto('/agents/1/edit')
  for (const heading of ['Identity & avatar', 'Instructions', 'Provider & model', 'Runtime & context', 'Skills', 'Lifecycle']) {
    await expect(page.getByRole('heading', { name: heading })).toBeAttached()
  }
  await expect(page.getByRole('button', { name: 'Save changes' }).first()).toBeVisible()
  await expect(page.getByRole('button', { name: 'Save changes' }).first()).toBeDisabled()
  await expect(page.getByText('No pending changes')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Markdown' })).toBeAttached()
})

test('uses provider catalog metadata for agent runtime controls', async ({ page }) => {
	await mockAPI(page, true)
	await page.goto('/providers')
	await expect(page.getByRole('heading', { name: 'Providers' })).toBeVisible()
	await expect(page.getByRole('link', { name: /Codex OAuth/ })).toContainText('1 models')
	await page.getByRole('link', { name: /Codex OAuth/ }).click()
	await expect(page.getByText('GPT-5.6 Sol', { exact: true })).toBeVisible()
	await expect(page.getByText('200,000 context')).toBeVisible()

	await page.goto('/agents/1/edit')
	await expect(page.getByRole('combobox').first()).toHaveValue('1')
	await expect(page.locator('input[list="provider-model-options"]')).toHaveValue('gpt-5.6-sol')
	await expect(page.locator('input[list="reasoning-effort-options"]')).toHaveAttribute('placeholder', 'Provider default (medium)')
	await expect(page.locator('#runtime .field').filter({ hasText: 'Context window' }).locator('input')).toHaveAttribute('placeholder', '200000')

	await page.getByRole('navigation', { name: 'Agent settings sections' }).getByText('Runtime & context').click()
	await expect(page).toHaveURL(/#runtime$/)
	await page.waitForTimeout(350)
	const heading = await page.getByRole('heading', { name: 'Runtime & context' }).boundingBox()
	const savebar = await page.locator('.settings-savebar').boundingBox()
	expect(heading && savebar && heading.y + heading.height <= savebar.y).toBeTruthy()
})

test('shows Telegram as an ordinary-agent integration', async ({ page }) => {
	await mockAPI(page, true)
	await page.goto('/settings/integrations')
	await expect(page.getByRole('heading', { name: 'Integrations' })).toBeVisible()
	await expect(page.getByText('Token configured')).toBeVisible()
	await expect(page.getByRole('combobox')).toHaveValue('1')
	await expect(page.getByRole('option', { name: 'Research Atlas · Codex OAuth' })).toBeAttached()
})
