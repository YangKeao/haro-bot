import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ExternalLink, GitBranch, Library, LoaderCircle, Plus, RefreshCw, Search, Sparkles, Trash2, X } from 'lucide-react'
import * as Dialog from '@radix-ui/react-dialog'
import { api, APIError } from '../api'

export default function Skills() {
  const client = useQueryClient()
  const [query, setQuery] = useState('')
  const [dialog, setDialog] = useState(false)
  const [url, setURL] = useState('')
  const [ref, setRef] = useState('main')
  const [subdir, setSubdir] = useState('')
  const [filters, setFilters] = useState('')
  const skills = useQuery({ queryKey: ['skills'], queryFn: api.skills })
  const sources = useQuery({ queryKey: ['skill-sources'], queryFn: api.skillSources })
  const refreshAll = () => { client.invalidateQueries({ queryKey: ['skills'] }); client.invalidateQueries({ queryKey: ['skill-sources'] }) }
  const add = useMutation({ mutationFn: () => api.addSkillSource({ url, ref, subdir, skill_filters: filters.split(',').map(v => v.trim()).filter(Boolean) }), onSuccess: () => { setDialog(false); setURL(''); refreshAll() } })
  const refresh = useMutation({ mutationFn: api.refreshSkillSource, onSuccess: refreshAll })
  const remove = useMutation({ mutationFn: api.deleteSkillSource, onSuccess: refreshAll })
  const visible = skills.data?.skills.filter(skill => !query || `${skill.name} ${skill.description}`.toLowerCase().includes(query.toLowerCase())) || []
  const submit = (event: FormEvent) => { event.preventDefault(); if (url.trim()) add.mutate() }
  return <main className="page scroll-page settings-page">
    <header className="page-header"><div><div className="eyebrow">Capabilities</div><h1>Skills library</h1><p className="muted">Install reusable instructions and decide which agents can access them.</p></div><button className="button primary" onClick={() => setDialog(true)}><Plus size={17} /> Add source</button></header>
    <div className="stats-row"><div><Library /><span><b>{skills.data?.skills.length || 0}</b> installed skills</span></div><div><GitBranch /><span><b>{sources.data?.sources.filter(source => source.status === 'active').length || 0}</b> active sources</span></div></div>
    <section className="settings-card skills-section"><div className="section-toolbar"><div><h2>Installed skills</h2><p>Available for assignment in agent settings.</p></div><div className="search-input"><Search /><input value={query} onChange={e => setQuery(e.target.value)} placeholder="Search skills" /></div></div>
      <div className="installed-skills">{skills.isLoading ? [1,2,3].map(i => <div className="skill-card skeleton" key={i} />) : visible.length ? visible.map(skill => <article className="skill-card" key={skill.name}><div className="skill-icon"><Sparkles /></div><div><h3>{skill.name}</h3><p>{skill.description}</p><span>{skill.version || skill.hash.slice(0, 8) || 'local'}</span></div></article>) : <div className="empty-inline wide"><Sparkles /><div><b>{query ? 'No matching skills' : 'No skills installed'}</b><p>{query ? 'Try a different search.' : 'Add a Git source to build your library.'}</p></div></div>}</div>
    </section>
    <section className="settings-card sources-section"><div className="section-toolbar"><div><h2>Sources</h2><p>Git repositories synchronized into the workspace.</p></div></div>
      <div className="source-list">{sources.isLoading ? <div className="source-row skeleton" /> : sources.data?.sources.length ? sources.data.sources.map(source => <article key={source.id} className={`source-row ${source.status !== 'active' ? 'archived' : ''}`}><div className="source-icon"><GitBranch /></div><div className="source-main"><a href={source.url} target="_blank" rel="noreferrer">{source.url}<ExternalLink size={13} /></a><div><span>{source.ref || 'main'}</span>{source.subdir && <span>{source.subdir}</span>}<span>{source.version?.slice(0, 10) || 'Not synced'}</span></div>{source.last_error && <small className="field-error">{source.last_error}</small>}</div><div className="row-actions"><button className="icon-button" disabled={refresh.isPending || source.status !== 'active'} onClick={() => refresh.mutate(source.id)} aria-label="Refresh source"><RefreshCw className={refresh.isPending ? 'spin' : ''} /></button><button className="icon-button danger" disabled={remove.isPending || source.status !== 'active'} onClick={() => remove.mutate(source.id)} aria-label="Delete source"><Trash2 /></button></div></article>) : <div className="empty-inline wide"><GitBranch /><div><b>No sources yet</b><p>Add a repository containing one or more SKILL.md files.</p></div></div>}</div>
    </section>
    <Dialog.Root open={dialog} onOpenChange={setDialog}><Dialog.Portal><Dialog.Overlay className="dialog-overlay" /><Dialog.Content className="dialog-content"><div className="dialog-head"><div><Dialog.Title>Add a skill source</Dialog.Title><Dialog.Description>Sync skills from a Git repository.</Dialog.Description></div><Dialog.Close className="icon-button"><X /></Dialog.Close></div><form onSubmit={submit}><label className="field"><span>Repository URL</span><input autoFocus value={url} onChange={e => setURL(e.target.value)} placeholder="https://github.com/org/skills.git" /></label><div className="form-grid"><label className="field"><span>Git ref</span><input value={ref} onChange={e => setRef(e.target.value)} placeholder="main" /></label><label className="field"><span>Subdirectory</span><input value={subdir} onChange={e => setSubdir(e.target.value)} placeholder="skills" /></label></div><label className="field"><span>Skill filters <em>Optional, comma separated</em></span><input value={filters} onChange={e => setFilters(e.target.value)} placeholder="research, release-notes" /></label>{add.error && <div className="form-error">{add.error instanceof APIError ? add.error.message : 'Could not add source'}</div>}<div className="dialog-actions"><Dialog.Close className="button ghost">Cancel</Dialog.Close><button className="button primary" disabled={!url.trim() || add.isPending}>{add.isPending ? <LoaderCircle className="spin" size={16} /> : <Plus size={16} />} Add and sync</button></div></form></Dialog.Content></Dialog.Portal></Dialog.Root>
  </main>
}
