import type { AgentInput, AgentProfile, Attachment, Guideline, Message, ModelCatalog, ProviderInput, ProviderProfile, RecentSession, RunEvent, Session, Skill, SkillSource, TelegramIntegration } from './types'

export class APIError extends Error {
  constructor(public status: number, public code: string, message: string) {
    super(message)
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    credentials: 'same-origin',
    ...init,
    headers: init?.body instanceof FormData ? init.headers : { 'Content-Type': 'application/json', ...init?.headers },
  })
  if (!response.ok) {
    let detail = { error: { code: 'request_failed', message: response.statusText } }
    try { detail = await response.json() } catch { /* response was not JSON */ }
    throw new APIError(response.status, detail.error.code, detail.error.message)
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

export const api = {
  auth: () => request<{ authenticated: boolean }>('/api/v1/auth/session'),
  login: (token: string) => request('/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ token }) }),
  logout: () => request('/api/v1/auth/logout', { method: 'POST', body: '{}' }),
  agents: (archived = false) => request<{ agents: AgentProfile[] }>(`/api/v1/agents?archived=${archived}`),
  agent: (id: number) => request<AgentProfile>(`/api/v1/agents/${id}`),
  createAgent: (input: AgentInput, avatar?: File, removeAvatar = false) => saveAgent('/api/v1/agents', 'POST', input, avatar, removeAvatar),
  updateAgent: (id: number, input: Partial<AgentInput>, avatar?: File, removeAvatar = false) => saveAgent(`/api/v1/agents/${id}`, 'PUT', input, avatar, removeAvatar),
  archiveAgent: (id: number, restore = false) => request(`/api/v1/agents/${id}/${restore ? 'restore' : 'archive'}`, { method: 'POST', body: '{}' }),
  providers: (archived = false) => request<{ providers: ProviderProfile[] }>(`/api/v1/providers?archived=${archived}`),
  provider: (id: number) => request<ProviderProfile>(`/api/v1/providers/${id}`),
  createProvider: (input: ProviderInput) => request<ProviderProfile>('/api/v1/providers', { method: 'POST', body: JSON.stringify(input) }),
  updateProvider: (id: number, input: ProviderInput) => request<ProviderProfile>(`/api/v1/providers/${id}`, { method: 'PUT', body: JSON.stringify(input) }),
  archiveProvider: (id: number, restore = false) => request(`/api/v1/providers/${id}/${restore ? 'restore' : 'archive'}`, { method: 'POST', body: '{}' }),
  providerModels: (id: number) => request<ModelCatalog>(`/api/v1/providers/${id}/models`),
  refreshProviderModels: (id: number) => request<ModelCatalog>(`/api/v1/providers/${id}/models/refresh`, { method: 'POST', body: '{}' }),
  telegramIntegration: () => request<TelegramIntegration>('/api/v1/integrations/telegram'),
  updateTelegramIntegration: (agentID: number | null) => request<TelegramIntegration>('/api/v1/integrations/telegram', { method: 'PUT', body: JSON.stringify({ agent_id: agentID }) }),
  recentSessions: (limit = 6) => request<{ sessions: RecentSession[] }>(`/api/v1/sessions/recent?limit=${limit}`),
  sessions: (agentID: number, archived = false) => request<{ sessions: Session[] }>(`/api/v1/agents/${agentID}/sessions?archived=${archived}`),
  session: (id: number) => request<{ session: Session; status: { State: string; CurrentTool: string; LLMModel: string; StartTime: string } }>(`/api/v1/sessions/${id}`),
  createSession: (agentID: number) => request<Session>(`/api/v1/agents/${agentID}/sessions`, { method: 'POST', body: '{}' }),
  renameSession: (id: number, title: string) => request(`/api/v1/sessions/${id}`, { method: 'PATCH', body: JSON.stringify({ title }) }),
  archiveSession: (id: number, restore = false) => request(`/api/v1/sessions/${id}/${restore ? 'restore' : 'archive'}`, { method: 'POST', body: '{}' }),
  messages: (id: number) => request<{ messages: Message[]; next_cursor?: number }>(`/api/v1/sessions/${id}/messages?limit=200`),
  cancel: (id: number) => request<{ cancelled: boolean }>(`/api/v1/sessions/${id}/cancel`, { method: 'POST', body: '{}' }),
  upload: async (sessionID: number, file: File): Promise<Attachment> => {
    const body = new FormData()
    body.append('file', file)
    return request(`/api/v1/sessions/${sessionID}/attachments`, { method: 'POST', body })
  },
  deleteAttachment: (id: string) => request(`/api/v1/attachments/${id}`, { method: 'DELETE' }),
  guideline: () => request<{ guidelines: Guideline | null }>('/api/v1/guidelines'),
  guidelineHistory: () => request<{ history: Guideline[] }>('/api/v1/guidelines/history'),
  updateGuideline: (content: string) => request<{ guidelines: Guideline }>('/api/v1/guidelines', { method: 'PUT', body: JSON.stringify({ content }) }),
  skills: () => request<{ skills: Skill[] }>('/api/v1/skills'),
  skillSources: () => request<{ sources: SkillSource[] }>('/api/v1/skill-sources?archived=true'),
  addSkillSource: (input: { url: string; ref: string; subdir: string; skill_filters: string[] }) => request('/api/v1/skill-sources', { method: 'POST', body: JSON.stringify(input) }),
  refreshSkillSource: (id: number) => request(`/api/v1/skill-sources/${id}/refresh`, { method: 'POST', body: '{}' }),
  deleteSkillSource: (id: number) => request(`/api/v1/skill-sources/${id}`, { method: 'DELETE' }),
}

function saveAgent(path: string, method: string, input: Partial<AgentInput>, avatar?: File, removeAvatar = false) {
  if (!avatar && !removeAvatar) return request<AgentProfile>(path, { method, body: JSON.stringify(input) })
  const body = new FormData()
  body.append('profile', JSON.stringify(input))
  if (avatar) body.append('avatar', avatar)
  if (removeAvatar) body.append('remove_avatar', 'true')
  return request<AgentProfile>(path, { method, body })
}

export async function streamRun(sessionID: number, content: string, attachmentIDs: string[], onEvent: (event: RunEvent) => void, signal: AbortSignal) {
  const response = await fetch(`/api/v1/sessions/${sessionID}/runs`, {
    method: 'POST', credentials: 'same-origin', signal,
    headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream' },
    body: JSON.stringify({ content, attachment_ids: attachmentIDs }),
  })
  if (!response.ok) {
    const detail = await response.json().catch(() => ({ error: { code: 'request_failed', message: response.statusText } }))
    throw new APIError(response.status, detail.error.code, detail.error.message)
  }
  if (!response.body) throw new APIError(500, 'stream_unavailable', 'Streaming response is unavailable')
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  while (true) {
    const { value, done } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    let boundary: number
    while ((boundary = buffer.indexOf('\n\n')) >= 0) {
      const block = buffer.slice(0, boundary)
      buffer = buffer.slice(boundary + 2)
      let event = 'message'
      let data = '{}'
      for (const line of block.split('\n')) {
        if (line.startsWith('event:')) event = line.slice(6).trim()
        if (line.startsWith('data:')) data = line.slice(5).trim()
      }
      onEvent({ event, data: JSON.parse(data) })
    }
  }
}
