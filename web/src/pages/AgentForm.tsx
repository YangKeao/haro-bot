import { useEffect, useMemo, useState } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Archive, ArrowLeft, Bot, Box, BrainCircuit, Check, CircleAlert, Cloud, Code2, ImagePlus, KeyRound, LoaderCircle,
	RefreshCw, RotateCcw, Save, Search, Settings2, Sparkles, Trash2, WandSparkles,
} from 'lucide-react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, APIError } from '../api'
import type { AgentInput, AgentProfile } from '../types'
import MarkdownEditor from '../components/MarkdownEditor'
import AgentEnvironmentEditor from '../components/AgentEnvironmentEditor'
import { AgentAvatar } from './Home'

const defaults: AgentInput = {
  name: '', description: '', icon: 'sparkles', color: '#2563eb', avatar_mode: 'icon', instructions: '',
  provider_id: 0, model: '', reasoning_effort_override: null, context_window_override: null, auto_compact_token_limit_override: null,
  effective_context_window_percent: 95, skill_names: [], sandbox_id: null,
}

const iconOptions = [
  { value: 'sparkles', label: 'Sparkles', Icon: Sparkles }, { value: 'bot', label: 'Bot', Icon: Bot },
  { value: 'search', label: 'Research', Icon: Search }, { value: 'code', label: 'Code', Icon: Code2 },
  { value: 'brain', label: 'Thinking', Icon: BrainCircuit },
]

const sections = [
  { id: 'identity', label: 'Identity', Icon: Bot },
  { id: 'instructions', label: 'Instructions', Icon: WandSparkles },
  { id: 'provider', label: 'Provider & model', Icon: Cloud },
  { id: 'runtime', label: 'Runtime & context', Icon: Settings2 },
  { id: 'sandbox', label: 'Sandbox', Icon: Box },
  { id: 'environment', label: 'Environment', Icon: KeyRound },
  { id: 'skills', label: 'Skills', Icon: Sparkles },
]

function valuesFromProfile(profile: AgentProfile): AgentInput {
  return {
    name: profile.name, description: profile.description, icon: profile.icon, color: profile.color.toLowerCase(),
    avatar_mode: profile.avatar_mode || 'icon', instructions: profile.instructions, provider_id: profile.provider_id,
    model: profile.model, reasoning_effort_override: profile.reasoning_effort_override,
    context_window_override: profile.context_window_override, auto_compact_token_limit_override: profile.auto_compact_token_limit_override,
    effective_context_window_percent: profile.effective_context_window_percent, skill_names: profile.skill_names, sandbox_id: profile.sandbox_id,
  }
}

export default function AgentForm() {
  const { agentID } = useParams()
  const id = agentID ? Number(agentID) : undefined
  const navigate = useNavigate()
  const client = useQueryClient()
  const [notice, setNotice] = useState<string>()
  const [saved, setSaved] = useState(false)
  const [avatarFile, setAvatarFile] = useState<File>()
  const [removeAvatar, setRemoveAvatar] = useState(false)
  const existing = useQuery({ queryKey: ['agent', id], queryFn: () => api.agent(id!), enabled: !!id })
  const providers = useQuery({ queryKey: ['providers'], queryFn: () => api.providers(false) })
  const skills = useQuery({ queryKey: ['skills'], queryFn: api.skills })
  const sandboxes = useQuery({ queryKey: ['sandboxes'], queryFn: api.sandboxes, retry: false })
  const { register, handleSubmit, reset, watch, setValue, control, formState: { errors, isDirty } } = useForm<AgentInput>({ defaultValues: defaults })

  useEffect(() => {
    if (existing.data) reset(valuesFromProfile(existing.data))
  }, [existing.data, reset])
  useEffect(() => {
    const warn = (event: BeforeUnloadEvent) => {
      if (!isDirty && !avatarFile && !removeAvatar) return
      event.preventDefault()
    }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [avatarFile, isDirty, removeAvatar])

  const previewURL = useMemo(() => avatarFile ? URL.createObjectURL(avatarFile) : existing.data?.avatar_url, [avatarFile, existing.data?.avatar_url])
  useEffect(() => () => { if (avatarFile && previewURL) URL.revokeObjectURL(previewURL) }, [avatarFile, previewURL])

  const save = useMutation({
    mutationFn: async (values: AgentInput) => {
      return id ? api.updateAgent(id, values, avatarFile, removeAvatar) : api.createAgent(values, avatarFile, removeAvatar)
    },
    onSuccess: profile => {
      client.invalidateQueries({ queryKey: ['agents'] })
      client.setQueryData(['agent', profile.id], profile)
      setAvatarFile(undefined); setRemoveAvatar(false)
      reset(valuesFromProfile(profile)); setSaved(true); setTimeout(() => setSaved(false), 2400)
      if (!id) navigate(`/agents/${profile.id}/sessions`)
    },
  })
  const archive = useMutation({
    mutationFn: () => api.archiveAgent(id!, !!existing.data?.archived_at),
    onSuccess: () => { client.invalidateQueries({ queryKey: ['agents'] }); client.invalidateQueries({ queryKey: ['recent-sessions'] }); existing.refetch() },
  })
  const selected = watch('skill_names') || []
  const name = watch('name')
  const color = watch('color')
  const icon = watch('icon')
  const avatarMode = watch('avatar_mode')
  const providerID = watch('provider_id')
  const modelID = watch('model')
  const sandboxID = watch('sandbox_id')
  const providerModels = useQuery({ queryKey: ['provider-models', providerID], queryFn: () => api.providerModels(providerID), enabled: providerID > 0 })
  const refreshCatalog = useMutation({
    mutationFn: () => api.refreshProviderModels(providerID),
    onSuccess: data => { client.setQueryData(['provider-models', providerID], data); setNotice(`Loaded ${data.models.length} models from the provider.`) },
    onError: error => setNotice(error instanceof APIError ? `${error.message} Manual values are still supported.` : 'Could not refresh models. Manual values are still supported.'),
  })
  const selectedModel = providerModels.data?.models.find(model => model.id === modelID)
  const canSave = Boolean(name.trim() && providerID > 0 && modelID.trim())
  const hasStoredAvatar = Boolean(existing.data?.avatar_url && !removeAvatar)
  const dirty = isDirty || Boolean(avatarFile) || removeAvatar
  const avatarAgent = { name: name || 'Agent', color: color || '#2563eb', icon: icon || 'sparkles', avatar_mode: avatarMode, avatar_url: previewURL }

  const selectAvatar = (file?: File) => {
    if (!file) return
    if (file.size > 2 * 1024 * 1024) { setNotice('Avatar images must be 2 MiB or smaller.'); return }
    setAvatarFile(file); setRemoveAvatar(false); setValue('avatar_mode', 'image', { shouldDirty: true })
  }
  const discard = () => {
    reset(existing.data ? valuesFromProfile(existing.data) : defaults)
    setAvatarFile(undefined); setRemoveAvatar(false); setNotice(undefined)
  }

  useEffect(() => {
    if (!id && !providerID && providers.data?.providers.length === 1) {
      setValue('provider_id', providers.data.providers[0].id, { shouldDirty: true })
    }
  }, [id, providerID, providers.data, setValue])
  useEffect(() => {
    if (providerModels.data?.stale && !refreshCatalog.isPending) refreshCatalog.mutate()
    // Refresh only when a newly selected provider reports a stale cache.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [providerID, providerModels.data?.stale])

  if (id && existing.isLoading) return <div className="page-loading"><LoaderCircle className="spin" /></div>
  return <main className="page scroll-page agent-settings-page">
    <header className="editor-header settings-main-header">
      <div className="header-row"><Link to={id ? `/agents/${id}/sessions` : '/'} className="icon-button" aria-label={id ? 'Back to conversations' : 'Back to home'}><ArrowLeft /></Link><div><nav className="breadcrumbs" aria-label="Breadcrumb"><Link to="/">Home</Link><span>/</span><span>Agents</span><span>/</span><span>{id ? existing.data?.name || 'Agent' : 'New'}</span></nav><div className="eyebrow">{id ? 'Agent settings' : 'New agent'}</div><h1>{id ? `Edit ${existing.data?.name ?? ''}` : 'Create an agent'}</h1><p className="muted">Everything this agent needs, on one page.</p></div></div>
      <button className="button primary header-save" onClick={handleSubmit(values => save.mutate(values))} disabled={!canSave || !dirty || save.isPending}>{save.isPending ? <LoaderCircle className="spin" size={16} /> : saved ? <Check size={16} /> : <Save size={16} />}{saved ? 'Saved' : id ? 'Save changes' : 'Create agent'}</button>
    </header>

    <div className="agent-settings-layout">
      <nav className="settings-outline" aria-label="Agent settings sections">{sections.map(section => <a key={section.id} href={`#${section.id}`}><section.Icon /><span>{section.label}</span></a>)}{id && <a href="#lifecycle"><Archive /><span>Lifecycle</span></a>}</nav>
      <form className="settings-form" onSubmit={handleSubmit(values => save.mutate(values))}>
        <section id="identity" className="form-section settings-section">
          <div className="section-heading"><h2>Identity & avatar</h2><p>Give this agent a recognizable presence across conversations.</p></div>
          <div className="avatar-settings">
            <AgentAvatar agent={avatarAgent} size="large" />
            <div className="avatar-controls"><div className="segmented-control" aria-label="Avatar type"><button type="button" className={avatarMode === 'icon' ? 'active' : ''} onClick={() => setValue('avatar_mode', 'icon', { shouldDirty: true })}>Built-in icon</button><button type="button" className={avatarMode === 'image' ? 'active' : ''} disabled={!avatarFile && !hasStoredAvatar} onClick={() => setValue('avatar_mode', 'image', { shouldDirty: true })}>Image</button></div><div className="avatar-actions"><label className="button secondary small"><ImagePlus size={15} /> {hasStoredAvatar || avatarFile ? 'Replace image' : 'Upload image'}<input type="file" hidden accept="image/jpeg,image/png,image/webp" onChange={event => selectAvatar(event.target.files?.[0])} /></label>{hasStoredAvatar && <button type="button" className="button ghost small danger-text" onClick={() => { setAvatarFile(undefined); setRemoveAvatar(true); setValue('avatar_mode', 'icon', { shouldDirty: true }) }}><Trash2 size={15} /> Remove image</button>}</div><small>JPEG, PNG or WebP, up to 2 MiB. Images are center-cropped.</small></div>
          </div>
          <div className="form-grid"><label className="field span-2"><span>Name</span><input autoFocus={!id} {...register('name', { required: 'Name is required', maxLength: { value: 128, message: 'Use at most 128 characters' } })} placeholder="Research partner" />{errors.name && <small className="field-error">{errors.name.message}</small>}</label>
            <label className="field span-2"><span>Description</span><textarea rows={3} {...register('description')} placeholder="What is this agent best at?" /></label>
            <div className="field span-2"><span>Built-in icon</span><div className="icon-picker">{iconOptions.map(option => <button type="button" key={option.value} className={icon === option.value ? 'active' : ''} onClick={() => { setValue('icon', option.value, { shouldDirty: true }); setValue('avatar_mode', 'icon', { shouldDirty: true }) }} aria-label={option.label}><option.Icon /><small>{option.label}</small></button>)}</div></div>
            <label className="field"><span>Agent color</span><div className="color-input"><input type="color" {...register('color')} /><input {...register('color')} /></div></label>
          </div>
        </section>

        <section id="instructions" className="form-section settings-section">
          <div className="section-heading"><h2>Instructions</h2><p>Appended after the workspace-wide guideline and applied from the next conversation turn.</p></div>
          <Controller control={control} name="instructions" render={({ field }) => <MarkdownEditor value={field.value} onChange={field.onChange} ariaLabel="Agent instructions" placeholder="Describe this agent's role, priorities, and constraints…" />} />
        </section>

        <section id="provider" className="form-section settings-section">
          <div className="section-heading"><h2>Provider & model</h2><p>Select a shared connection, then choose a discovered model or enter one manually.</p></div>
          {!providers.isLoading && !providers.data?.providers.length ? <div className="inline-alert warning"><CircleAlert /><div><b>A provider is required</b><p><Link to="/settings/providers/new">Create a provider</Link> before saving this agent.</p></div></div> : <div className="form-grid">
            <div className="field span-2"><span>Provider</span><Controller control={control} name="provider_id" rules={{ min: { value: 1, message: 'Choose a provider' } }} render={({ field }) => <select value={field.value || ''} onChange={event => { field.onChange(Number(event.target.value)); setValue('model', '', { shouldDirty: true }) }}><option value="">Choose a provider…</option>{providers.data?.providers.map(provider => <option key={provider.id} value={provider.id}>{provider.name} · {provider.model_count} models</option>)}</select>} />{errors.provider_id && <small className="field-error">{errors.provider_id.message}</small>}<small>Base URL, API key and prompt format are managed in <Link to="/settings#providers">Providers</Link>.</small></div>
            <label className="field span-2"><span>Model <em>{providerModels.data?.models.length ? `${providerModels.data.models.length} discovered` : 'Manual entry allowed'}</em></span><input list="provider-model-options" {...register('model', { required: 'Model is required' })} placeholder="Enter or select a model ID" /><datalist id="provider-model-options">{providerModels.data?.models.map(model => <option key={model.id} value={model.id}>{model.display_name || model.id}</option>)}</datalist>{errors.model && <small className="field-error">{errors.model.message}</small>}</label>
          </div>}
          {providerID > 0 && <div className="test-row"><button type="button" className="button secondary" disabled={refreshCatalog.isPending} onClick={() => refreshCatalog.mutate()}>{refreshCatalog.isPending ? <LoaderCircle className="spin" size={16} /> : <RefreshCw size={16} />} Refresh models</button>{providerModels.data?.fetched_at && <span className="muted">Updated {new Date(providerModels.data.fetched_at).toLocaleString()}</span>}{notice && <span className={refreshCatalog.isError ? 'field-error' : 'success-text'}>{notice}</span>}</div>}
        </section>

        <section id="runtime" className="form-section settings-section">
          <div className="section-heading"><h2>Runtime & context</h2><p>Automatic values follow the selected model whenever its Provider catalog is refreshed.</p></div>
          <div className="form-grid">
            <label className="field span-2"><span>Reasoning effort <em>Optional override</em></span><input list="reasoning-effort-options" {...register('reasoning_effort_override', { setValueAs: value => value?.trim() || null })} placeholder={`Provider default${selectedModel?.default_reasoning_effort ? ` (${selectedModel.default_reasoning_effort})` : ''}`} /><datalist id="reasoning-effort-options">{selectedModel?.reasoning_efforts?.map(option => <option key={option.value} value={option.value}>{option.description}</option>)}</datalist><small>Leave blank to omit the parameter and use the Provider default. Custom Provider values are accepted.</small></label>
            <div className="field"><span>Context window <em>Optional override</em></span><Controller control={control} name="context_window_override" render={({ field }) => <input type="number" min="1" value={field.value ?? ''} onChange={event => field.onChange(event.target.value ? Number(event.target.value) : null)} placeholder={String(selectedModel?.context_window || selectedModel?.max_context_window || 'Auto')} />} /><small>{selectedModel?.context_window || selectedModel?.max_context_window ? `Auto: ${(selectedModel.context_window || selectedModel.max_context_window)?.toLocaleString()} tokens` : 'Auto: unavailable; manual fallback supported'}</small></div>
            <div className="field"><span>Auto-compact limit <em>Optional override</em></span><Controller control={control} name="auto_compact_token_limit_override" render={({ field }) => <input type="number" min="1" value={field.value ?? ''} onChange={event => field.onChange(event.target.value ? Number(event.target.value) : null)} placeholder={String(selectedModel?.auto_compact_token_limit || 'Auto')} />} /><small>{selectedModel?.auto_compact_token_limit ? `Auto: ${selectedModel.auto_compact_token_limit.toLocaleString()} tokens` : 'Derived from the context window when available'}</small></div>
            <label className="field"><span>Effective window %</span><input type="number" min="1" max="100" {...register('effective_context_window_percent', { valueAsNumber: true, min: 1, max: 100 })} /></label>
          </div>
        </section>

        <section id="sandbox" className="form-section settings-section">
          <div className="section-heading"><h2>Sandbox</h2><p>Attach a persistent Kubernetes workspace for command execution. A Sandbox may be shared by several agents.</p></div>
          <div className="form-grid"><div className="field span-2"><span>Execution workspace <em>Optional</em></span><Controller control={control} name="sandbox_id" render={({ field }) => <select value={field.value ?? ''} onChange={event => field.onChange(event.target.value ? Number(event.target.value) : null)}><option value="">No command execution</option>{sandboxes.data?.sandboxes.map(item => <option value={item.id} key={item.id}>{item.name} · {item.runtime_status || item.desired_state}</option>)}</select>} /><small>Environment variables below are injected per process and are never stored in the Sandbox Pod.</small></div></div>
          {sandboxes.error instanceof APIError && sandboxes.error.code === 'sandbox_disabled' ? <div className="inline-alert warning"><CircleAlert /><div><b>Sandbox support is disabled</b><p>Enable it in the server configuration to run code.</p></div></div> : <div className="test-row"><Link className="button secondary" to="/sandboxes"><Box /> Manage sandboxes</Link>{sandboxID && <span className="muted">Changes take effect for the agent's next tool call.</span>}</div>}
        </section>

        <section id="environment" className="form-section settings-section">
          <div className="section-heading"><h2>Environment variables</h2><p>Configure database credentials and runtime settings for this agent without exposing them in its instructions.</p></div>
          <AgentEnvironmentEditor agentID={id} sandboxID={sandboxID} />
        </section>

        <section id="skills" className="form-section settings-section">
          <div className="section-heading"><h2>Skills</h2><p>An empty selection is valid for a focused base agent.</p></div>
          <div className="skill-picker">{skills.isLoading ? <div className="skeleton tall" /> : skills.data?.skills.length ? skills.data.skills.map(skill => {
            const checked = selected.includes(skill.name)
            return <label key={skill.name} className={`skill-option ${checked ? 'selected' : ''}`}><input type="checkbox" checked={checked} onChange={() => setValue('skill_names', checked ? selected.filter(name => name !== skill.name) : [...selected, skill.name], { shouldDirty: true })} /><span className="skill-check">{checked && <Check size={15} />}</span><div><b>{skill.name}</b><p>{skill.description}</p></div></label>
          }) : <div className="empty-inline"><Sparkles /><div><b>No skills installed yet</b><p>You can save this agent without skills and add them later.</p></div></div>}</div>
          <div className="selection-summary">{selected.length ? `${selected.length} skill${selected.length === 1 ? '' : 's'} selected` : 'No skills selected'}</div>
        </section>

        {id && <section id="lifecycle" className="form-section settings-section danger-section"><div className="section-heading"><h2>Lifecycle</h2><p>Archived agents keep their settings and conversations but leave the active workspace.</p></div><button type="button" className="button secondary" onClick={() => archive.mutate()} disabled={archive.isPending}>{existing.data?.archived_at ? <RotateCcw size={16} /> : <Archive size={16} />}{existing.data?.archived_at ? 'Restore agent' : 'Archive agent'}</button></section>}

        {save.error && <div className="form-error" role="alert">{save.error instanceof APIError ? save.error.message : 'Could not save agent'}</div>}
        <footer className="settings-savebar"><div><b>{dirty ? 'Unsaved changes' : saved ? 'All changes saved' : 'No pending changes'}</b><span>{dirty ? 'Review the sections above, then save once.' : 'Agent settings are up to date.'}</span></div><div><button type="button" className="button ghost" disabled={!dirty || save.isPending} onClick={discard}>Discard</button><button className="button primary" disabled={!canSave || !dirty || save.isPending}>{save.isPending ? <LoaderCircle className="spin" size={16} /> : <Save size={16} />}{id ? 'Save changes' : 'Create agent'}</button></div></footer>
      </form>
    </div>
  </main>
}
