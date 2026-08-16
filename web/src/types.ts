export interface AgentProfile {
  id: number
  provider_id: number
  provider_name: string
  name: string
  description: string
  icon: string
  color: string
  avatar_mode: 'icon' | 'image'
  avatar_url?: string
  instructions: string
  model: string
  reasoning_effort_override: string | null
  provider_default_reasoning_effort?: string
  context_window_override: number | null
  resolved_context_window: number
  auto_compact_token_limit_override: number | null
  resolved_auto_compact_token_limit: number
  effective_context_window_percent: number
  skill_names: string[]
  archived_at?: string
  created_at: string
  updated_at: string
}

export type AgentInput = Omit<AgentProfile, 'id' | 'provider_name' | 'avatar_url' | 'provider_default_reasoning_effort' | 'resolved_context_window' | 'resolved_auto_compact_token_limit' | 'archived_at' | 'created_at' | 'updated_at'>

export interface ReasoningEffortOption {
  value: string
  description?: string
}

export interface ModelCapability {
  id: string
  display_name?: string
  description?: string
  context_window?: number
  max_context_window?: number
  auto_compact_token_limit?: number
  default_reasoning_effort?: string
  reasoning_efforts?: ReasoningEffortOption[]
  input_modalities?: string[]
}

export interface ProviderProfile {
  id: number
  name: string
  base_url: string
  api_key_configured: boolean
  prompt_format: 'openai' | 'claude'
  model_count: number
  models_fetched_at?: string
  models_last_error?: string
  catalog_stale: boolean
  archived_at?: string
  created_at: string
  updated_at: string
}

export interface ProviderInput {
  name: string
  base_url: string
  api_key?: string
  clear_api_key?: boolean
  prompt_format: 'openai' | 'claude'
}

export interface ModelCatalog {
  models: ModelCapability[]
  fetched_at?: string
  last_error?: string
  stale: boolean
}

export interface TelegramIntegration {
  token_configured: boolean
  agent_id: number | null
}

export interface Session {
  id: number
  agent_id: number
  title: string
  archived_at?: string
  created_at: string
  updated_at: string
}

export interface RecentSession extends Omit<Session, 'archived_at'> {
  agent: Pick<AgentProfile, 'id' | 'name' | 'icon' | 'color' | 'avatar_mode' | 'avatar_url'>
}

export interface Attachment {
  id: string
  session_id: number
  message_id?: number
  name: string
  mime_type: string
  size_bytes: number
  created_at: string
}

export interface ToolCall {
  id: string
  type: string
  function: { name: string; arguments: string }
}

export interface MessageMetadata {
  reasoning_content?: string
  tool_call_id?: string
  tool_calls?: ToolCall[]
  status?: string
  attachment_ids?: string[]
}

export interface Message {
  id: number | string
  session_id: number
  role: 'user' | 'assistant' | 'tool'
  content: string
  metadata?: MessageMetadata
  attachments?: Attachment[]
  created_at: string
}

export interface Skill {
  name: string
  description: string
  version: string
  hash: string
}

export interface SkillSource {
  id: number
  url: string
  ref: string
  subdir: string
  skill_filters: string[]
  status: string
  version: string
  last_sync_at?: string
  last_error?: string
}

export interface Guideline {
  id: number
  content: string
  version: number
  is_active: boolean
  created_at: string
  updated_at: string
}

export interface RunEvent {
  event: string
  data: Record<string, unknown>
}
