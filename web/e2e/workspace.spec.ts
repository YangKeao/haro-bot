import { expect, test, type Page, type Route } from '@playwright/test'

const agent = {
  id: 1,
	provider_id: 1,
	sandbox_id: 1,
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

const sandbox = {
	id: 1,
	name: 'Data Lab',
	description: 'A shared workspace for database analysis and small utilities.',
	image: 'ghcr.io/yangkeao/haro-bot-sandbox:latest',
	cpu_limit_millis: 2000,
	memory_limit_mib: 4096,
	ephemeral_storage_mib: 4096,
	workspace_storage_mib: 10240,
	desired_state: 'Running',
	revision: 2,
	applied_revision: 1,
	pending_restart: true,
	kubernetes_name: 'haro-data-lab-a1b2c3d4',
	runtime_status: 'Ready',
	runtime_details: { state: 'Ready', message: 'Sandbox Pod is ready.', observed_at: '2026-08-14T01:00:00Z', pod: { name: 'haro-data-lab-a1b2c3d4', uid: 'pod-1', image: 'sandbox:test', phase: 'Running', created_at: '2026-08-14T01:00:00Z', started_at: '2026-08-14T01:00:05Z', ready: true, restart_count: 0 } },
	agent_ids: [1],
	created_at: '2026-08-14T01:00:00Z',
	updated_at: '2026-08-14T01:00:00Z',
}

const sandboxConfig = {
	default_image: 'ghcr.io/yangkeao/haro-bot-sandbox@sha256:new-default',
	defaults: { cpu_limit_millis: 2000, memory_limit_mib: 4096, ephemeral_storage_mib: 4096, workspace_storage_mib: 10240 },
	maximums: { cpu_limit_millis: 8000, memory_limit_mib: 16384, ephemeral_storage_mib: 32768, workspace_storage_mib: 102400, running: 10 },
}

const sandboxProcess = {
	id: 'process-1', sandbox_id: 1, agent_id: 1, session_id: 10,
	command: 'python3 analyze.py --database sales', tty: false, status: 'running', pid: 42,
	started_at: '2026-08-14T01:00:00Z', duration_millis: 15342,
	cpu_percent: 12.4, memory_bytes: 73400320, output: 'Connected to MySQL\nProcessed 4,200 rows\n',
}

const finishedProcess = {
	id: 'process-2', sandbox_id: 1, agent_id: 1, session_id: 10,
	command: 'printf complete', status: 'exited', pid: 43, exit_code: 0,
	started_at: '2026-08-14T01:00:00Z', finished_at: '2026-08-14T01:00:00Z', duration_millis: 42,
	cpu_percent: 0, memory_bytes: 0, output: 'complete',
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
	title: 'Investigate tidb\\_enable\\_check\\_constraint',
	agent: { id: agent.id, name: agent.name, icon: agent.icon, color: agent.color, avatar_mode: agent.avatar_mode },
}

const activeSkillSource = {
	id: 30003, url: 'https://github.com/example/skills.git', ref: 'master', subdir: '', skill_filters: ['web-search'],
	status: 'active', version: 'abcdef1234567890', last_error: '',
}

const removedSkillSource = {
	...activeSkillSource, id: 30002, ref: 'main', skill_filters: [], status: 'deleted', version: '',
}

async function mockAPI(page: Page, initiallyAuthenticated = false) {
	let authenticated = initiallyAuthenticated
	let agentArchived = true
	let sessionArchived = true
	let currentSkillSource = { ...activeSkillSource }
	let removedSkillSourceStatus = 'deleted'
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
		if (path === '/api/v1/sandboxes/events') return route.fulfill({ contentType: 'text/event-stream', body: `event: snapshot\ndata: ${JSON.stringify({ sandboxes: [sandbox] })}\n\n` })
		if (path === '/api/v1/sandboxes') return route.fulfill({ json: { sandboxes: [sandbox], config: sandboxConfig } })
		if (path === '/api/v1/sandboxes/1') return route.fulfill({ json: { sandbox, config: sandboxConfig } })
		if (path === '/api/v1/sandboxes/1/apply' || path === '/api/v1/sandboxes/1/restart' || path === '/api/v1/sandboxes/1/start' || path === '/api/v1/sandboxes/1/pause') return route.fulfill({ status: 202, json: sandbox })
		if (path === '/api/v1/agents/1/environment') return route.fulfill({ json: { variables: [{ name: 'MYSQL_HOST', value: 'database.internal', secret: false, has_value: true }, { name: 'MYSQL_PASSWORD', secret: true, has_value: true }] } })
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
		{ id: 2, session_id: 10, role: 'assistant', content: '', metadata: { tool_calls: [{ id: 'call-1', type: 'function', function: { name: 'exec_command', arguments: '{"cmd":"printf complete"}' } }] }, created_at: '2026-08-14T01:00:30Z' },
		{ id: 3, session_id: 10, role: 'tool', content: 'Process ID: process-2\nStatus: exited\nWall time: 0.0420 seconds\nProcess exited with code 0\nOutput:\ncomplete', metadata: { tool_call_id: 'call-1', status: 'ok' }, created_at: '2026-08-14T01:00:31Z' },
		{ id: 4, session_id: 10, role: 'assistant', content: 'The strongest signal is sustained demand with moderating growth.', created_at: '2026-08-14T01:01:00Z' },
		{ id: 5, session_id: 10, role: 'assistant', content: '```sql\nSELECT * FROM users WHERE enabled = TRUE;\n```', created_at: '2026-08-14T01:01:30Z' },
      ] } })
    }
		if (path === '/api/v1/sessions/10/processes') return route.fulfill({ json: { processes: [sandboxProcess, finishedProcess] } })
		if (path === '/api/v1/processes/process-1/signal' || path === '/api/v1/processes/process-1/stdin') return route.fulfill({ json: sandboxProcess })
		if (path === '/api/v1/sessions/11/messages') return route.fulfill({ json: { messages: [] } })
		if (path === '/api/v1/skills') return route.fulfill({ json: { skills: [{ name: 'web-search', description: 'Search public sources', version: '1', hash: 'abc' }] } })
		if (path === '/api/v1/guidelines') return route.fulfill({ json: { guidelines: { id: 1, content: 'Be precise.', version: 1, is_active: true, created_at: '2026-08-14T01:00:00Z', updated_at: '2026-08-14T01:00:00Z' } } })
		if (path === '/api/v1/guidelines/history') return route.fulfill({ json: { history: [] } })
		if (path === '/api/v1/skill-sources' && request.method() === 'GET') {
			return route.fulfill({ json: { sources: [currentSkillSource, { ...removedSkillSource, status: removedSkillSourceStatus }] } })
		}
		if (path === '/api/v1/skill-sources/30003' && request.method() === 'PUT') {
			const input = request.postDataJSON()
			currentSkillSource = { ...currentSkillSource, ...input, skill_filters: [...input.skill_filters].sort(), version: 'fedcba9876543210' }
			return route.fulfill({ json: { source: currentSkillSource } })
		}
		if (path === '/api/v1/skill-sources/30002/restore') {
			removedSkillSourceStatus = 'active'
			return route.fulfill({ json: { source: { ...removedSkillSource, status: 'active' } } })
		}
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
  const recentLink = page.getByRole('link', { name: /Investigate tidb_enable_check_constraint/ })
  await expect(recentLink).not.toContainText('\\')
  if (process.env.REVIEW_SCREENSHOTS) await recentLink.screenshot({ path: '/tmp/haro-commonmark-title.png' })
  await recentLink.click()
  await expect(page.getByRole('heading', { name: 'Quarterly market brief' })).toBeVisible()
  await expect(page.getByText('The strongest signal is sustained demand with moderating growth.')).toBeVisible()
})

test('renders highlighted code blocks as a single dark surface', async ({ page }) => {
	await mockAPI(page, true)
	await page.goto('/agents/1/sessions/10')
	const code = page.locator('.markdown pre code.hljs').filter({ hasText: 'SELECT * FROM users' })
	await expect(code).toBeVisible()
	if (process.env.REVIEW_SCREENSHOTS) await code.locator('..').screenshot({ path: '/tmp/haro-highlighted-code.png' })
	const styles = await code.evaluate(element => {
		type BrowserStyle = { backgroundColor: string; padding: string }
		const browser = globalThis as unknown as { getComputedStyle: (target: unknown) => BrowserStyle }
		const codeElement = element as unknown as { closest: (selector: string) => unknown }
		const codeStyle = browser.getComputedStyle(element)
		const preStyle = browser.getComputedStyle(codeElement.closest('pre'))
		return {
			codeBackground: codeStyle.backgroundColor,
			codePadding: codeStyle.padding,
			preBackground: preStyle.backgroundColor,
		}
	})
	expect(styles.codeBackground).toBe('rgba(0, 0, 0, 0)')
	expect(styles.codePadding).toBe('0px')
	expect(styles.preBackground).toBe('rgb(38, 39, 34)')
	expect(await code.locator('span').count()).toBeGreaterThan(0)
})

test('finds and restores archived agents', async ({ page }) => {
  await mockAPI(page, true)
  await page.goto('/')
  await page.getByRole('button', { name: 'Archived' }).click()
  await expect(page.getByText('Legacy Analyst')).toBeVisible()
  await page.getByRole('button', { name: 'Restore' }).click()
  await expect(page.locator('.nav-rail .primary-agent-row').filter({ hasText: 'Legacy Analyst' })).toBeVisible()
})

test('edits active skill sources and restores removed sources separately', async ({ page }) => {
	await mockAPI(page, true)
	await page.goto('/skills')
	await expect(page.getByRole('link', { name: activeSkillSource.url })).toHaveCount(1)
	await expect(page.getByText('1 active sources')).toBeVisible()

	await page.getByRole('button', { name: /Removed sources/ }).click()
	await expect(page.getByRole('link', { name: activeSkillSource.url })).toHaveCount(2)
	await page.getByRole('button', { name: `Edit ${activeSkillSource.url}` }).click()
	await page.getByLabel('Git ref').fill('release')
	await page.getByLabel(/Skill filters/).fill('web-search, release-notes')
	await page.getByRole('button', { name: /Save and sync/ }).click()
	await expect(page.getByText('release', { exact: true })).toBeVisible()
	await expect(page.getByText('Includes: release-notes, web-search')).toBeVisible()

	await page.getByRole('button', { name: 'Restore' }).click()
	await expect(page.getByText('2 active sources')).toBeVisible()
	await expect(page.getByRole('button', { name: /Removed sources/ })).toHaveCount(0)
})

test('keeps agent and session navigation usable on mobile', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await mockAPI(page, true)
  await page.goto('/agents/1/sessions/10')
	const chatBounds = await page.locator('.chat-panel').boundingBox()
	expect(chatBounds?.width).toBeGreaterThanOrEqual(389)
	if (process.env.REVIEW_SCREENSHOTS) await page.screenshot({ path: '/tmp/haro-chat-mobile.png', fullPage: true })

  await page.getByRole('button', { name: 'Open navigation' }).click()
  const navigationDrawer = page.getByRole('dialog').filter({ hasText: 'Haro' })
  await expect(navigationDrawer.getByText('Agents', { exact: true })).toBeVisible()
  await expect(navigationDrawer.getByRole('link', { name: /Research Atlas/ })).toBeVisible()
	if (process.env.REVIEW_SCREENSHOTS) { await page.waitForTimeout(250); await page.screenshot({ path: '/tmp/haro-navigation-mobile.png', fullPage: true }) }
  await navigationDrawer.getByRole('button', { name: 'Close navigation' }).click()
  await page.getByRole('button', { name: 'Open conversations' }).click()
  const conversationDrawer = page.getByRole('dialog').filter({ hasText: 'Conversations' })
  await expect(conversationDrawer.getByText('Quarterly market brief')).toBeVisible()
	if (process.env.REVIEW_SCREENSHOTS) { await page.waitForTimeout(250); await page.screenshot({ path: '/tmp/haro-conversations-mobile.png', fullPage: true }) }
  await conversationDrawer.getByRole('button', { name: 'Archived conversations' }).click()
  await expect(conversationDrawer.getByText('Archived design review')).toBeVisible()
  await conversationDrawer.getByRole('button', { name: 'Restore Archived design review' }).click()
  await page.waitForURL(/\/agents\/1\/sessions\/11$/)
})

test('keeps the conversation context visible on tablet but hides it on agent settings', async ({ page }) => {
  await page.setViewportSize({ width: 1024, height: 768 })
  await mockAPI(page, true)
  await page.goto('/agents/1/sessions/10')
  await expect(page.locator('.desktop-conversation-sidebar')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Open conversations' })).toBeHidden()
  const chatBounds = await page.locator('.chat-panel').boundingBox()
  expect(chatBounds?.width).toBeGreaterThan(700)
  if (process.env.REVIEW_SCREENSHOTS) await page.screenshot({ path: '/tmp/haro-chat-tablet.png', fullPage: true })

  await page.goto('/agents/1/edit')
  await expect(page.locator('.desktop-conversation-sidebar')).toHaveCount(0)
  await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toContainText('Research Atlas')
})

test('uses labeled navigation and persists appearance preferences', async ({ page }) => {
  await mockAPI(page, true)
  await page.goto('/')
  await expect(page.getByRole('link', { name: 'Home', exact: true })).toBeVisible()
  await expect(page.locator('.nav-rail').getByRole('link', { name: 'New agent' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Guidelines' })).toHaveCount(0)
  await expect(page.getByRole('link', { name: 'Sandboxes' })).toBeVisible()
  if (process.env.REVIEW_SCREENSHOTS) await page.screenshot({ path: '/tmp/haro-navigation-desktop.png', fullPage: true })
  await page.getByRole('link', { name: 'Settings' }).click()
  await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible()
  for (const heading of ['Appearance', 'Providers', 'Global guideline', 'Skills library', 'Integrations']) {
    await expect(page.getByRole('heading', { name: heading, exact: true })).toBeAttached()
  }
  if (process.env.REVIEW_SCREENSHOTS) await page.screenshot({ path: '/tmp/haro-settings-desktop.png', fullPage: true })
  await page.getByRole('radio', { name: /Dusty rose/ }).click()
  await expect(page.locator('html')).toHaveAttribute('data-accent', 'rose')
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('data-accent', 'rose')
})

test('uses a labeled navigation drawer on mobile', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await mockAPI(page, true)
  await page.goto('/')
  await expect(page.getByRole('button', { name: 'Open navigation' })).toBeVisible()
  await page.getByRole('button', { name: 'Open navigation' }).click()
  await expect(page.getByRole('navigation', { name: 'Mobile navigation' }).getByRole('link', { name: 'Home' })).toBeVisible()
  await expect(page.getByRole('navigation', { name: 'Mobile workspace settings' }).getByRole('link', { name: 'Settings' })).toBeVisible()
  await expect(page.getByRole('dialog').getByRole('link', { name: /Research Atlas/ })).toBeVisible()
  await expect(page.getByText('Accent color')).toHaveCount(0)
  if (process.env.REVIEW_SCREENSHOTS) await page.screenshot({ path: '/tmp/haro-navigation-mobile.png', fullPage: true })
  await page.getByRole('button', { name: 'Close navigation' }).click()
})

test('shows every agent setting section on one page', async ({ page }) => {
  await mockAPI(page, true)
  await page.goto('/agents/1/edit')
  for (const heading of ['Identity & avatar', 'Instructions', 'Provider & model', 'Runtime & context', 'Sandbox', 'Environment variables', 'Skills', 'Lifecycle']) {
    await expect(page.getByRole('heading', { name: heading })).toBeAttached()
  }
  await expect(page.getByRole('button', { name: 'Save changes' }).first()).toBeVisible()
  await expect(page.getByRole('button', { name: 'Save changes' }).first()).toBeDisabled()
  await expect(page.getByText('No pending changes')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Markdown' })).toBeAttached()
  await expect(page.locator('.desktop-conversation-sidebar')).toHaveCount(0)
  await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toContainText('Agents')
})

test('keeps the skills section in view while selecting agent skills', async ({ page }) => {
	await mockAPI(page, true)
	await page.goto('/agents/1/edit')
	const skillsHeading = page.getByRole('heading', { name: 'Skills', exact: true })
	await page.getByRole('navigation', { name: 'Agent settings sections' }).getByText('Skills', { exact: true }).click()
	await expect(page).toHaveURL(/#skills$/)
	await expect(skillsHeading).toBeInViewport()

	const option = page.locator('.skill-option').filter({ hasText: 'web-search' })
	await expect(option.locator('input')).toBeChecked()
	await option.click()
	await expect(option.locator('input')).not.toBeChecked()
	await expect.poll(() => page.evaluate(() => (globalThis as unknown as { scrollY: number }).scrollY)).toBe(0)
	await expect(skillsHeading).toBeInViewport()
})

test('manages persistent sandboxes and exposes session processes', async ({ page }) => {
	await mockAPI(page, true)
	await page.goto('/sandboxes')
	await expect(page.getByRole('heading', { name: 'Sandboxes' })).toBeVisible()
	await expect(page.getByRole('link', { name: /Data Lab/ })).toContainText('Changes waiting to be applied')
	if (process.env.REVIEW_SCREENSHOTS) await page.screenshot({ path: '/tmp/haro-sandboxes-desktop.png', fullPage: true })
	await page.getByRole('link', { name: /Data Lab/ }).click()
	await expect(page.getByRole('heading', { name: 'Resources & persistence' })).toBeVisible()
	await expect(page.getByText('Changes are not active yet')).toBeVisible()
	await expect(page.getByRole('button', { name: 'Apply changes' })).toBeEnabled()
	await expect(page.getByRole('button', { name: 'Restart' })).toBeEnabled()
	await expect(page.locator('#runtime').getByRole('link', { name: 'Open terminal' })).toBeVisible()
	await expect(page.locator('#operations').getByRole('link', { name: 'Open terminal' })).toHaveCount(0)
	if (process.env.REVIEW_SCREENSHOTS) await page.locator('#runtime').screenshot({ path: '/tmp/haro-sandbox-runtime.png' })
	await page.getByRole('button', { name: 'Update to default' }).click()
	await expect(page.getByLabel('OCI image')).toHaveValue(sandboxConfig.default_image)
	await expect(page.getByRole('button', { name: 'Save changes' }).first()).toBeEnabled()
	await expect(page.getByRole('button', { name: 'Apply changes' })).toBeDisabled()
	if (process.env.REVIEW_SCREENSHOTS) await page.screenshot({ path: '/tmp/haro-sandbox-form-desktop.png', fullPage: true })

	await page.goto('/agents/1/sessions/10')
	await expect(page.getByText('Sandbox processes')).toBeVisible()
	await expect(page.locator('.process-panel')).not.toContainText('printf complete')
	await expect(page.locator('.inline-process')).toContainText('printf complete')
	await expect(page.locator('.tool-call').filter({ hasText: 'exec_command' })).toHaveCount(0)
	await page.locator('.inline-process summary').click()
	await expect(page.locator('.inline-process')).toContainText('complete')
	await page.getByText('python3 analyze.py --database sales').click()
	await expect(page.getByText('Processed 4,200 rows')).toBeVisible()
	await expect(page.getByRole('button', { name: 'TERM' })).toBeVisible()
	await expect(page.locator('.process-panel input[placeholder="Send stdin…"]')).toHaveCount(0)
	if (process.env.REVIEW_SCREENSHOTS) {
		await page.screenshot({ path: '/tmp/haro-process-desktop.png', fullPage: true })
		await page.setViewportSize({ width: 390, height: 844 })
		await page.waitForTimeout(350)
		await page.screenshot({ path: '/tmp/haro-process-mobile.png', fullPage: true })
	}
})

test('opens an interactive Sandbox terminal', async ({ page }) => {
	await mockAPI(page, true)
	const clientMessages: string[] = []
	await page.routeWebSocket('**/api/v1/sandboxes/1/terminal', socket => {
		socket.onMessage(message => {
			const text = message.toString()
			clientMessages.push(text)
			const parsed = JSON.parse(text) as { type: string; data?: string }
			if (parsed.type === 'input' && parsed.data) socket.send(JSON.stringify({ type: 'output', data: parsed.data }))
		})
		socket.send(JSON.stringify({ type: 'output', data: 'Connected to /workspace\r\n$ ' }))
	})

	await page.goto('/sandboxes/1/terminal')
	await expect(page.getByRole('heading', { name: 'Data Lab' })).toBeVisible()
	await expect(page.getByText('connected', { exact: true })).toBeVisible()
	await expect(page.locator('.xterm-rows')).toContainText('Connected to /workspace')
	await page.locator('.terminal-surface').click()
	await page.keyboard.type('pwd')
	await expect.poll(() => clientMessages.some(message => JSON.parse(message).type === 'resize')).toBeTruthy()
	await expect.poll(() => clientMessages.map(message => JSON.parse(message).data || '').join('').includes('pwd')).toBeTruthy()
	await page.getByRole('button', { name: 'Disconnect' }).click()
	await expect(page.getByText('disconnected', { exact: true })).toBeVisible()
})

test('uses provider catalog metadata for agent runtime controls', async ({ page }) => {
	await mockAPI(page, true)
	await page.goto('/providers')
	await page.waitForURL(/\/settings#providers$/)
	await expect(page.getByRole('heading', { name: 'Providers' })).toBeVisible()
	await expect(page.getByRole('link', { name: /Codex OAuth/ })).toContainText('1 models')
	await page.getByRole('link', { name: /Codex OAuth/ }).click()
	await page.waitForURL(/\/settings\/providers\/1\/edit$/)
	await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toContainText('Settings/Providers/Codex OAuth')
	await expect(page.getByText('GPT-5.6 Sol', { exact: true })).toBeVisible()
	await expect(page.getByText('200,000 context')).toBeVisible()
	await page.getByRole('link', { name: 'Back to providers' }).click()
	await page.waitForURL(/\/settings#providers$/)

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
