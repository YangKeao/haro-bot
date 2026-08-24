import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Archive, ChevronDown, ExternalLink, GitBranch, Library, LoaderCircle, Pencil, Plus, RefreshCw, RotateCcw, Search, Sparkles, Trash2, X } from 'lucide-react'
import * as Dialog from '@radix-ui/react-dialog'
import { api, APIError } from '../api'
import type { SkillSource, SkillSourceInput } from '../types'

function mutationMessage(error: Error | null, fallback: string) {
  if (!error) return ''
  return error instanceof APIError ? error.message : fallback
}

export default function Skills() {
  const client = useQueryClient()
  const [query, setQuery] = useState('')
  const [dialog, setDialog] = useState(false)
  const [editing, setEditing] = useState<SkillSource | null>(null)
  const [showRemoved, setShowRemoved] = useState(false)
  const [url, setURL] = useState('')
  const [ref, setRef] = useState('main')
  const [subdir, setSubdir] = useState('')
  const [filters, setFilters] = useState('')
  const skills = useQuery({ queryKey: ['skills'], queryFn: api.skills })
  const sources = useQuery({ queryKey: ['skill-sources'], queryFn: api.skillSources })
  const refreshAll = () => {
    client.invalidateQueries({ queryKey: ['skills'] })
    client.invalidateQueries({ queryKey: ['skill-sources'] })
  }
  const input = (): SkillSourceInput => ({
    url,
    ref,
    subdir,
    skill_filters: filters.split(',').map(value => value.trim()).filter(Boolean),
  })
  const save = useMutation({
    mutationFn: async () => {
      if (editing) await api.updateSkillSource(editing.id, input())
      else await api.addSkillSource(input())
    },
    onSuccess: () => { closeDialog(); refreshAll() },
  })
  const refresh = useMutation({ mutationFn: api.refreshSkillSource, onSuccess: refreshAll })
  const remove = useMutation({ mutationFn: api.deleteSkillSource, onSuccess: refreshAll })
  const restore = useMutation({ mutationFn: api.restoreSkillSource, onSuccess: refreshAll })
  const resetForm = () => {
    setEditing(null)
    setURL('')
    setRef('main')
    setSubdir('')
    setFilters('')
    save.reset()
  }
  const closeDialog = () => {
    setDialog(false)
    resetForm()
  }
  const openAdd = () => {
    resetForm()
    setDialog(true)
  }
  const openEdit = (source: SkillSource) => {
    save.reset()
    setEditing(source)
    setURL(source.url)
    setRef(source.ref || 'main')
    setSubdir(source.subdir || '')
    setFilters((source.skill_filters ?? []).join(', '))
    setDialog(true)
  }
  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (url.trim()) save.mutate()
  }
  const allSources = sources.data?.sources ?? []
  const activeSources = allSources.filter(source => source.status === 'active')
  const removedSources = allSources.filter(source => source.status !== 'active')
  const visible = skills.data?.skills.filter(skill => !query || `${skill.name} ${skill.description}`.toLowerCase().includes(query.toLowerCase())) || []
  const actionError = mutationMessage(refresh.error, 'Could not refresh source')
    || mutationMessage(remove.error, 'Could not remove source')
    || mutationMessage(restore.error, 'Could not restore source')

  return <main className="page scroll-page settings-page">
    <header className="page-header"><div><div className="eyebrow">Capabilities</div><h1>Skills library</h1><p className="muted">Install reusable instructions and decide which agents can access them.</p></div><button className="button primary" onClick={openAdd}><Plus size={17} /> Add source</button></header>
    <div className="stats-row"><div><Library /><span><b>{skills.data?.skills.length || 0}</b> installed skills</span></div><div><GitBranch /><span><b>{activeSources.length}</b> active sources</span></div></div>
    <section className="settings-card skills-section"><div className="section-toolbar"><div><h2>Installed skills</h2><p>Available for assignment in agent settings.</p></div><div className="search-input"><Search /><input value={query} onChange={event => setQuery(event.target.value)} placeholder="Search skills" /></div></div>
      <div className="installed-skills">{skills.isLoading ? [1,2,3].map(item => <div className="skill-card skeleton" key={item} />) : visible.length ? visible.map(skill => <article className="skill-card" key={skill.name}><div className="skill-icon"><Sparkles /></div><div><h3>{skill.name}</h3><p>{skill.description}</p><span>{skill.version || skill.hash.slice(0, 8) || 'local'}</span></div></article>) : <div className="empty-inline wide"><Sparkles /><div><b>{query ? 'No matching skills' : 'No skills installed'}</b><p>{query ? 'Try a different search.' : 'Add a Git source to build your library.'}</p></div></div>}</div>
    </section>
    <section className="settings-card sources-section"><div className="section-toolbar"><div><h2>Sources</h2><p>Git repositories synchronized into the workspace.</p></div></div>
      <div className="source-list">{sources.isLoading ? <div className="source-row skeleton" /> : activeSources.length ? activeSources.map(source => <article key={source.id} className="source-row"><div className="source-icon"><GitBranch /></div><SourceDetails source={source} /><div className="row-actions"><button className="icon-button" disabled={save.isPending || refresh.isPending || remove.isPending || restore.isPending} onClick={() => openEdit(source)} aria-label={`Edit ${source.url}`}><Pencil /></button><button className="icon-button" disabled={refresh.isPending} onClick={() => refresh.mutate(source.id)} aria-label={`Refresh ${source.url}`}><RefreshCw className={refresh.isPending && refresh.variables === source.id ? 'spin' : ''} /></button><button className="icon-button danger" disabled={remove.isPending} onClick={() => remove.mutate(source.id)} aria-label={`Remove ${source.url}`}><Trash2 /></button></div></article>) : <div className="empty-inline wide"><GitBranch /><div><b>No active sources</b><p>Add a repository containing one or more SKILL.md files.</p></div></div>}</div>
      {actionError && <div className="form-error source-action-error" role="alert">{actionError}</div>}
      {removedSources.length > 0 && <div className="removed-sources"><button className="removed-sources-toggle" onClick={() => setShowRemoved(value => !value)} aria-expanded={showRemoved}><Archive /><span>Removed sources</span><b>{removedSources.length}</b><ChevronDown className={showRemoved ? 'open' : ''} /></button>{showRemoved && <div className="source-list removed-source-list">{removedSources.map(source => <article key={source.id} className="source-row removed"><div className="source-icon"><Archive /></div><SourceDetails source={source} /><button className="button secondary small" disabled={restore.isPending} onClick={() => restore.mutate(source.id)}>{restore.isPending && restore.variables === source.id ? <LoaderCircle className="spin" size={14} /> : <RotateCcw size={14} />} Restore</button></article>)}</div>}</div>}
    </section>
    <Dialog.Root open={dialog} onOpenChange={open => { if (!open) closeDialog() }}><Dialog.Portal><Dialog.Overlay className="dialog-overlay" /><Dialog.Content className="dialog-content"><div className="dialog-head"><div><Dialog.Title>{editing ? 'Edit skill source' : 'Add a skill source'}</Dialog.Title><Dialog.Description>{editing ? 'Update the repository configuration and synchronize it now.' : 'Sync skills from a Git repository.'}</Dialog.Description></div><Dialog.Close className="icon-button"><X /></Dialog.Close></div><form onSubmit={submit}><label className="field"><span>Repository URL</span><input autoFocus value={url} onChange={event => setURL(event.target.value)} placeholder="https://github.com/org/skills.git" /></label><div className="form-grid"><label className="field"><span>Git ref</span><input value={ref} onChange={event => setRef(event.target.value)} placeholder="main" /></label><label className="field"><span>Subdirectory</span><input value={subdir} onChange={event => setSubdir(event.target.value)} placeholder="skills" /></label></div><label className="field"><span>Skill filters <em>Optional, comma separated</em></span><input value={filters} onChange={event => setFilters(event.target.value)} placeholder="research, release-notes" /></label>{save.error && <div className="form-error" role="alert">{mutationMessage(save.error, editing ? 'Could not update source' : 'Could not add source')}</div>}<div className="dialog-actions"><Dialog.Close className="button ghost">Cancel</Dialog.Close><button className="button primary" disabled={!url.trim() || save.isPending}>{save.isPending ? <LoaderCircle className="spin" size={16} /> : editing ? <Pencil size={16} /> : <Plus size={16} />} {editing ? 'Save and sync' : 'Add and sync'}</button></div></form></Dialog.Content></Dialog.Portal></Dialog.Root>
  </main>
}

function SourceDetails({ source }: { source: SkillSource }) {
  const filters = source.skill_filters ?? []
  return <div className="source-main"><a href={source.url} target="_blank" rel="noreferrer">{source.url}<ExternalLink size={13} /></a><div className="source-facts"><span>{source.ref || 'main'}</span>{source.subdir && <span>{source.subdir}</span>}<span>{source.version?.slice(0, 10) || 'Not synced'}</span></div><div className="source-filters"><span>{filters.length ? `Includes: ${filters.join(', ')}` : 'All skills'}</span></div>{source.last_error && <small className="field-error">{source.last_error}</small>}</div>
}
