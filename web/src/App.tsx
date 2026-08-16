import { lazy, Suspense, useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Navigate, Route, Routes } from 'react-router-dom'
import { ArrowRight, LoaderCircle, LockKeyhole, Sparkles } from 'lucide-react'
import { api, APIError } from './api'
import Shell from './components/Shell'

const Home = lazy(() => import('./pages/Home'))
const AgentForm = lazy(() => import('./pages/AgentForm'))
const Chat = lazy(() => import('./pages/Chat'))
const Guidelines = lazy(() => import('./pages/Guidelines'))
const Skills = lazy(() => import('./pages/Skills'))
const Providers = lazy(() => import('./pages/Providers'))
const ProviderForm = lazy(() => import('./pages/ProviderForm'))
const Integrations = lazy(() => import('./pages/Integrations'))

function Login() {
  const [token, setToken] = useState('')
  const client = useQueryClient()
  const login = useMutation({
    mutationFn: () => api.login(token),
    onSuccess: () => client.invalidateQueries({ queryKey: ['auth'] }),
  })
  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (token.trim()) login.mutate()
  }
  return <main className="login-page">
    <div className="ambient ambient-one" /><div className="ambient ambient-two" />
    <section className="login-card" aria-labelledby="login-title">
      <div className="brand-mark large"><Sparkles size={25} /></div>
      <div className="eyebrow">Haro workspace</div>
      <h1 id="login-title">Welcome back</h1>
      <p className="muted">Enter the access token configured for this workspace.</p>
      <form onSubmit={submit}>
        <label className="field-label" htmlFor="access-token">Access token</label>
        <div className="input-with-icon"><LockKeyhole size={17} /><input id="access-token" type="password" autoFocus autoComplete="current-password" value={token} onChange={e => setToken(e.target.value)} placeholder="••••••••••••••••" /></div>
        {login.error && <div className="form-error" role="alert">{login.error instanceof APIError ? login.error.message : 'Could not sign in'}</div>}
        <button className="button primary wide" disabled={!token.trim() || login.isPending}>
          {login.isPending ? <LoaderCircle className="spin" size={17} /> : <>Open workspace <ArrowRight size={17} /></>}
        </button>
      </form>
      <p className="login-footnote">Your token stays in a secure, HTTP-only session cookie.</p>
    </section>
  </main>
}

function AuthGate() {
  const auth = useQuery({ queryKey: ['auth'], queryFn: api.auth, retry: false })
  if (auth.isLoading) return <div className="center-screen"><div className="brand-mark pulse"><Sparkles /></div></div>
  if (auth.isError) return <Login />
  return <Shell />
}

export default function App() {
  return <Routes>
    <Route path="/*" element={<AuthGate />} />
  </Routes>
}

export function AppRoutes() {
	return <Suspense fallback={<div className="page-loading"><LoaderCircle className="spin" /></div>}><Routes>
    <Route index element={<Home />} />
    <Route path="agents/new" element={<AgentForm />} />
    <Route path="agents/:agentID/edit" element={<AgentForm />} />
    <Route path="agents/:agentID/sessions/:sessionID?" element={<Chat />} />
    <Route path="guidelines" element={<Guidelines />} />
    <Route path="skills" element={<Skills />} />
    <Route path="providers" element={<Providers />} />
    <Route path="providers/new" element={<ProviderForm />} />
    <Route path="providers/:providerID/edit" element={<ProviderForm />} />
    <Route path="settings/integrations" element={<Integrations />} />
    <Route path="*" element={<Navigate to="/" replace />} />
	</Routes></Suspense>
}
