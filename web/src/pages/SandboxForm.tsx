import { useEffect, useState, type ReactNode } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Box, Check, CircleAlert, Cpu, Database, HardDrive, LoaderCircle, Pause, Play, RefreshCw, Save, Trash2, Users } from 'lucide-react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, APIError } from '../api'
import type { SandboxInput, SandboxProfile } from '../types'
import { AgentAvatar } from './Home'

const fallback: SandboxInput = { name: '', description: '', image: '', cpu_limit_millis: 2000, memory_limit_mib: 4096, ephemeral_storage_mib: 4096, workspace_storage_mib: 10240, agent_ids: [] }

function valuesFromProfile(item: SandboxProfile): SandboxInput {
  return { name: item.name, description: item.description, image: item.image, cpu_limit_millis: item.cpu_limit_millis, memory_limit_mib: item.memory_limit_mib, ephemeral_storage_mib: item.ephemeral_storage_mib, workspace_storage_mib: item.workspace_storage_mib, agent_ids: item.agent_ids }
}

export default function SandboxForm() {
  const { sandboxID } = useParams()
  const id = sandboxID ? Number(sandboxID) : undefined
  const navigate = useNavigate()
  const client = useQueryClient()
  const [saved, setSaved] = useState(false)
  const [confirmName, setConfirmName] = useState('')
  const existing = useQuery({ queryKey: ['sandbox', id], queryFn: () => api.sandbox(id!), enabled: !!id, retry: false })
  const listing = useQuery({ queryKey: ['sandboxes'], queryFn: api.sandboxes, enabled: !id, retry: false })
  const agents = useQuery({ queryKey: ['agents'], queryFn: () => api.agents() })
  const config = existing.data?.config || listing.data?.config
  const { register, control, handleSubmit, reset, setValue, watch, formState: { isDirty, errors } } = useForm<SandboxInput>({ defaultValues: fallback })

  useEffect(() => { if (existing.data) reset(valuesFromProfile(existing.data.sandbox)) }, [existing.data, reset])
  useEffect(() => {
    if (!id && config) reset({ ...fallback, image: config.default_image, ...config.defaults, agent_ids: [] })
  }, [config, id, reset])

  const save = useMutation({
    mutationFn: (values: SandboxInput) => id ? api.updateSandbox(id, values) : api.createSandbox(values),
    onSuccess: item => { client.invalidateQueries({ queryKey: ['sandboxes'] }); client.setQueryData(['sandbox', item.id], { sandbox: item, config }); reset(valuesFromProfile(item)); setSaved(true); setTimeout(() => setSaved(false), 2200); if (!id) navigate(`/sandboxes/${item.id}/edit`, { replace: true }) },
  })
  const action = useMutation({
    mutationFn: async (kind: 'apply' | 'start' | 'pause' | 'reset' | 'delete') => {
      if (!id) return
      if (kind === 'apply') return api.applySandbox(id)
      if (kind === 'start') return api.startSandbox(id)
      if (kind === 'pause') return api.pauseSandbox(id)
      if (kind === 'reset') return api.resetSandboxWorkspace(id, confirmName)
      return api.deleteSandbox(id, confirmName)
    },
    onSuccess: (_, kind) => { client.invalidateQueries({ queryKey: ['sandboxes'] }); client.invalidateQueries({ queryKey: ['sandbox', id] }); setConfirmName(''); if (kind === 'delete') navigate('/sandboxes') },
  })

  const selected = watch('agent_ids') || []
  const profile = existing.data?.sandbox
  const maximums = config?.maximums
  const loadError = existing.error || listing.error
  const disabled = loadError instanceof APIError && loadError.code === 'sandbox_disabled'
  const canSave = Boolean(watch('name')?.trim() && watch('image')?.trim())
  if ((id && existing.isLoading) || (!id && listing.isLoading)) return <div className="page-loading"><LoaderCircle className="spin" /></div>
  if (disabled) return <main className="page scroll-page settings-page"><div className="empty-state card"><CircleAlert /><h2>Sandbox support is disabled</h2><p>Enable it in Haro's configuration before creating a workspace.</p><Link className="button secondary" to="/sandboxes">Back to sandboxes</Link></div></main>
  return <main className="page scroll-page agent-settings-page">
    <header className="editor-header settings-main-header"><div className="header-row"><Link to="/sandboxes" className="icon-button" aria-label="Back"><ArrowLeft /></Link><div><div className="eyebrow">Execution workspace</div><h1>{id ? `Edit ${profile?.name || ''}` : 'Create a sandbox'}</h1><p className="muted">Runtime changes are staged until you explicitly apply them.</p></div></div><button className="button primary header-save" disabled={!canSave || !isDirty || save.isPending} onClick={handleSubmit(values => save.mutate(values))}>{save.isPending ? <LoaderCircle className="spin" /> : saved ? <Check /> : <Save />}{saved ? 'Saved' : id ? 'Save changes' : 'Create sandbox'}</button></header>
    <div className="agent-settings-layout"><nav className="settings-outline"><a href="#identity"><Box /><span>Identity & image</span></a><a href="#resources"><Cpu /><span>Resources</span></a><a href="#agents"><Users /><span>Agents</span></a>{id && <><a href="#operations"><Play /><span>Operations</span></a><a href="#danger"><Trash2 /><span>Danger zone</span></a></>}</nav>
      <form className="settings-form" onSubmit={handleSubmit(values => save.mutate(values))}>
        {profile?.last_error && <div className="inline-alert warning"><CircleAlert /><div><b>Provisioning error</b><p>{profile.last_error}</p></div></div>}
        {profile?.pending_restart && <div className="inline-alert warning"><RefreshCw /><div><b>Changes are not active yet</b><p>Save only updates the desired configuration. Use Apply & restart below when you are ready.</p></div></div>}
        <section id="identity" className="form-section settings-section"><div className="section-heading"><h2>Identity & image</h2><p>The image must contain <code>/bin/sh</code>. Haro injects its runtime without requiring changes to the image.</p></div><div className="form-grid"><label className="field"><span>Name</span><input autoFocus={!id} {...register('name', { required: 'Name is required', maxLength: 128 })} placeholder="Data workspace" />{errors.name && <small className="field-error">{errors.name.message}</small>}</label><label className="field"><span>OCI image</span><input className="code-textarea" {...register('image', { required: 'Image is required' })} placeholder="ghcr.io/yangkeao/haro-bot-sandbox:latest" />{errors.image && <small className="field-error">{errors.image.message}</small>}</label><label className="field span-2"><span>Description</span><textarea rows={3} {...register('description')} placeholder="What should agents use this workspace for?" /></label></div></section>
        <section id="resources" className="form-section settings-section"><div className="section-heading"><h2>Resources & persistence</h2><p>Processes have no automatic timeout. The workspace volume survives pause and restart.</p></div><div className="form-grid"><ResourceField label="CPU limit (millicores)" name="cpu_limit_millis" icon={<Cpu />} maximum={maximums?.cpu_limit_millis} register={register} /><ResourceField label="Memory limit (MiB)" name="memory_limit_mib" icon={<Database />} maximum={maximums?.memory_limit_mib} register={register} /><ResourceField label="Ephemeral storage (MiB)" name="ephemeral_storage_mib" icon={<HardDrive />} maximum={maximums?.ephemeral_storage_mib} register={register} /><ResourceField label="Persistent workspace (MiB)" name="workspace_storage_mib" icon={<HardDrive />} maximum={maximums?.workspace_storage_mib} disabled={!!id} register={register} /></div>{id && <p className="field-note">Persistent workspace size is immutable in this version. Create a new Sandbox to choose a different size.</p>}</section>
        <section id="agents" className="form-section settings-section"><div className="section-heading"><h2>Assigned agents</h2><p>Agents can use at most one Sandbox. Assigning one here moves it from its previous Sandbox. Sharing is a trust boundary: assigned agents can manage each other's processes and inspect the same files and process environment.</p></div><Controller control={control} name="agent_ids" render={() => <div className="sandbox-agent-picker">{agents.data?.agents.map(agent => { const checked = selected.includes(agent.id); return <label className={checked ? 'selected' : ''} key={agent.id}><input type="checkbox" checked={checked} onChange={() => setValue('agent_ids', checked ? selected.filter(value => value !== agent.id) : [...selected, agent.id], { shouldDirty: true })} /><AgentAvatar agent={agent} /><span><b>{agent.name}</b><small>{agent.model}</small></span><i>{checked && <Check />}</i></label> })}</div>} />{!agents.data?.agents.length && <div className="empty-inline"><Users /><div><b>No agents available</b><p>Create an agent now or assign one later.</p></div></div>}</section>
        {id && <section id="operations" className="form-section settings-section"><div className="section-heading"><h2>Lifecycle</h2><p>Pause deletes the Pod while keeping the workspace volume. In-container root filesystem changes are not persistent.</p></div><div className="sandbox-operation-row"><div><span className={`status-pill ${profile?.runtime_status === 'Ready' ? 'success' : 'warning'}`}>{profile?.runtime_status || profile?.desired_state}</span><small>Kubernetes resource: {profile?.kubernetes_name}</small></div><div>{profile?.desired_state === 'Running' ? <button type="button" className="button secondary" onClick={() => action.mutate('pause')} disabled={action.isPending}><Pause /> Pause</button> : <button type="button" className="button secondary" onClick={() => action.mutate('start')} disabled={action.isPending}><Play /> Start</button>}<button type="button" className="button primary" onClick={() => action.mutate('apply')} disabled={action.isPending}><RefreshCw /> Apply & restart</button></div></div></section>}
        {id && <section id="danger" className="form-section settings-section danger-section"><div className="section-heading"><h2>Danger zone</h2><p>Reset permanently deletes everything in <code>/workspace</code>. Delete removes the Sandbox and its persistent volume.</p></div><label className="field"><span>Type <b>{profile?.name}</b> to confirm</span><input value={confirmName} onChange={event => setConfirmName(event.target.value)} /></label><div className="danger-actions"><button type="button" className="button secondary danger-text" disabled={confirmName !== profile?.name || action.isPending || profile?.desired_state !== 'Suspended'} onClick={() => action.mutate('reset')}><RefreshCw /> Reset workspace</button><button type="button" className="button secondary danger-text" disabled={confirmName !== profile?.name || action.isPending} onClick={() => action.mutate('delete')}><Trash2 /> Delete sandbox</button></div><small className="field-note">Workspace reset requires the Sandbox to be paused first.</small></section>}
        {(save.error || action.error || loadError) && <div className="form-error">{formatError(save.error || action.error || loadError)}</div>}
        <footer className="settings-savebar"><div><b>{isDirty ? 'Unsaved changes' : saved ? 'All changes saved' : 'No pending changes'}</b><span>{isDirty ? 'Save changes before applying them to Kubernetes.' : 'The desired configuration is up to date.'}</span></div><div><button type="button" className="button ghost" disabled={!isDirty} onClick={() => reset(profile ? valuesFromProfile(profile) : { ...fallback, image: config?.default_image || '', ...(config?.defaults || {}), agent_ids: [] })}>Discard</button><button className="button primary" disabled={!canSave || !isDirty || save.isPending}><Save /> {id ? 'Save changes' : 'Create sandbox'}</button></div></footer>
      </form>
    </div>
  </main>
}

function ResourceField({ label, name, maximum, disabled, register, icon }: { label: string; name: keyof Pick<SandboxInput, 'cpu_limit_millis' | 'memory_limit_mib' | 'ephemeral_storage_mib' | 'workspace_storage_mib'>; maximum?: number; disabled?: boolean; register: ReturnType<typeof useForm<SandboxInput>>['register']; icon: ReactNode }) {
  return <label className="field resource-field"><span>{icon}{label}<em>{maximum ? `Max ${maximum.toLocaleString()}` : ''}</em></span><input type="number" min="1" max={maximum} disabled={disabled} {...register(name, { valueAsNumber: true, required: true, min: 1, max: maximum })} /></label>
}

function formatError(error: unknown) { return error instanceof APIError ? error.message : error instanceof Error ? error.message : 'The operation failed.' }
