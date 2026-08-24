import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Archive, Check, Cloud, KeyRound, LoaderCircle, RefreshCw, RotateCcw, Save } from 'lucide-react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useForm } from 'react-hook-form'
import { api, APIError } from '../api'
import type { ProviderInput } from '../types'

const defaults: ProviderInput = { name: '', base_url: '', api_key: '', prompt_format: 'openai' }

export default function ProviderForm() {
  const { providerID } = useParams()
  const id = providerID ? Number(providerID) : undefined
  const navigate = useNavigate()
  const client = useQueryClient()
  const [saved, setSaved] = useState(false)
  const [clearKey, setClearKey] = useState(false)
  const existing = useQuery({ queryKey: ['provider', id], queryFn: () => api.provider(id!), enabled: !!id })
  const catalog = useQuery({ queryKey: ['provider-models', id], queryFn: () => api.providerModels(id!), enabled: !!id })
  const { register, handleSubmit, reset, watch, formState: { errors, isDirty } } = useForm<ProviderInput>({ defaultValues: defaults })

  useEffect(() => {
    if (existing.data) reset({ name: existing.data.name, base_url: existing.data.base_url, api_key: '', prompt_format: existing.data.prompt_format })
  }, [existing.data, reset])

  const refresh = useMutation({
    mutationFn: (providerID: number) => api.refreshProviderModels(providerID),
    onSuccess: (data, providerID) => {
      client.setQueryData(['provider-models', providerID], data)
      client.invalidateQueries({ queryKey: ['providers'] })
      client.invalidateQueries({ queryKey: ['agents'] })
    },
  })
  const save = useMutation({
    mutationFn: async (values: ProviderInput) => {
      const payload = { ...values }
      if (id && !payload.api_key && !clearKey) delete payload.api_key
      if (id && clearKey) payload.clear_api_key = true
      const provider = id ? await api.updateProvider(id, payload) : await api.createProvider(payload)
      try { await refresh.mutateAsync(provider.id) } catch { /* provider remains usable with manual model settings */ }
      return provider
    },
    onSuccess: provider => {
      client.invalidateQueries({ queryKey: ['providers'] })
      client.setQueryData(['provider', provider.id], provider)
      setClearKey(false); reset({ name: provider.name, base_url: provider.base_url, api_key: '', prompt_format: provider.prompt_format })
      setSaved(true); setTimeout(() => setSaved(false), 2400)
      if (!id) navigate(`/settings/providers/${provider.id}/edit`, { replace: true })
    },
  })
  const archive = useMutation({
    mutationFn: () => api.archiveProvider(id!, !!existing.data?.archived_at),
    onSuccess: () => { client.invalidateQueries({ queryKey: ['providers'] }); existing.refetch() },
  })
  const name = watch('name')
  const dirty = isDirty || clearKey
  if (id && existing.isLoading) return <div className="page-loading"><LoaderCircle className="spin" /></div>

  return <main className="page scroll-page agent-settings-page">
    <header className="editor-header settings-main-header"><div className="header-row"><Link to="/settings#providers" className="icon-button" aria-label="Back to providers"><ArrowLeft /></Link><div><nav className="breadcrumbs" aria-label="Breadcrumb"><Link to="/settings">Settings</Link><span>/</span><Link to="/settings#providers">Providers</Link><span>/</span><span>{id ? existing.data?.name || 'Provider' : 'New'}</span></nav><div className="eyebrow">{id ? 'Provider settings' : 'New provider'}</div><h1>{id ? `Edit ${existing.data?.name ?? ''}` : 'Connect a provider'}</h1><p className="muted">One connection can power any number of agents.</p></div></div><button className="button primary header-save" onClick={handleSubmit(values => save.mutate(values))} disabled={!name.trim() || !dirty || save.isPending}>{save.isPending ? <LoaderCircle className="spin" size={16} /> : saved ? <Check size={16} /> : <Save size={16} />}{saved ? 'Saved' : 'Save provider'}</button></header>
    <form className="provider-settings" onSubmit={handleSubmit(values => save.mutate(values))}>
      <section className="form-section settings-section"><div className="section-heading"><h2>Connection</h2><p>Haro calls the OpenAI-compatible model and Chat Completions APIs at this URL.</p></div><div className="form-grid">
        <label className="field span-2"><span>Name</span><input autoFocus={!id} {...register('name', { required: 'Name is required', maxLength: 128 })} placeholder="OpenAI OAuth" />{errors.name && <small className="field-error">{errors.name.message}</small>}</label>
        <label className="field span-2"><span>Base URL</span><input {...register('base_url', { required: 'Base URL is required' })} placeholder="https://openai-oauth.example/v1" />{errors.base_url && <small className="field-error">{errors.base_url.message}</small>}</label>
        <label className="field span-2"><span>API key <em>{existing.data?.api_key_configured ? 'A key is already stored' : 'Optional; blank keys are supported'}</em></span><input type="password" autoComplete="off" {...register('api_key')} placeholder={existing.data?.api_key_configured ? 'Leave blank to keep the stored key' : 'Optional'} /></label>
        {existing.data?.api_key_configured && <label className="check-row span-2"><input type="checkbox" checked={clearKey} onChange={event => setClearKey(event.target.checked)} /> Clear the stored API key when saving</label>}
        <label className="field"><span>Prompt format</span><select {...register('prompt_format')}><option value="openai">OpenAI / Markdown</option><option value="claude">Claude / XML</option></select></label>
      </div></section>

      {id && <section className="form-section settings-section"><div className="section-heading model-catalog-heading"><div><h2>Model catalog</h2><p>Capabilities are suggestions; agents may still use manually entered values.</p></div><button type="button" className="button secondary" onClick={() => refresh.mutate(id)} disabled={refresh.isPending}>{refresh.isPending ? <LoaderCircle className="spin" size={16} /> : <RefreshCw size={16} />} Refresh models</button></div>
        {refresh.error && <div className="inline-alert warning"><KeyRound /> <div><b>Discovery failed</b><p>{refresh.error instanceof APIError ? refresh.error.message : 'Could not load the model catalog.'} You can still configure model values manually.</p></div></div>}
        <div className="catalog-summary"><span><Cloud size={16} /> {catalog.data?.models.length ?? existing.data?.model_count ?? 0} models</span><span>{catalog.data?.fetched_at ? `Updated ${new Date(catalog.data.fetched_at).toLocaleString()}` : 'Not fetched yet'}</span></div>
        {catalog.data?.models.length ? <div className="model-catalog-list">{catalog.data.models.map(model => <article key={model.id}><div><b>{model.display_name || model.id}</b>{model.display_name && <code>{model.id}</code>}<p>{model.description || 'No description supplied by the provider.'}</p></div><div className="model-facts">{(model.context_window || model.max_context_window) && <span>{(model.context_window || model.max_context_window)?.toLocaleString()} context</span>}{model.reasoning_efforts?.length ? <span>{model.reasoning_efforts.map(item => item.value).join(' · ')}</span> : <span>No reasoning metadata</span>}</div></article>)}</div> : <div className="empty-inline"><RefreshCw /><div><b>No cached catalog</b><p>Save the provider and refresh models, or enter a model manually in an agent.</p></div></div>}
      </section>}

      {id && <section className="form-section settings-section danger-section"><div className="section-heading"><h2>Lifecycle</h2><p>A provider in use by an active agent cannot be archived.</p></div><button type="button" className="button secondary" onClick={() => archive.mutate()} disabled={archive.isPending}>{existing.data?.archived_at ? <RotateCcw size={16} /> : <Archive size={16} />}{existing.data?.archived_at ? 'Restore provider' : 'Archive provider'}</button>{archive.error && <div className="form-error">{archive.error instanceof APIError ? archive.error.message : 'Could not update provider'}</div>}</section>}
      {save.error && <div className="form-error">{save.error instanceof APIError ? save.error.message : 'Could not save provider'}</div>}
      <footer className="settings-savebar"><div><b>{dirty ? 'Unsaved changes' : saved ? 'All changes saved' : 'No pending changes'}</b><span>{refresh.isPending ? 'Saving succeeded; discovering models…' : 'Connection settings are shared by linked agents.'}</span></div><div><button type="button" className="button ghost" disabled={!dirty || save.isPending} onClick={() => reset(existing.data ? { name: existing.data.name, base_url: existing.data.base_url, api_key: '', prompt_format: existing.data.prompt_format } : defaults)}>Discard</button><button className="button primary" disabled={!name.trim() || !dirty || save.isPending}>{save.isPending ? <LoaderCircle className="spin" size={16} /> : <Save size={16} />} Save provider</button></div></footer>
    </form>
  </main>
}
