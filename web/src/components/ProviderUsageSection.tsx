import { useQuery } from '@tanstack/react-query'
import { CircleAlert, Clock3, Gauge, LoaderCircle, RefreshCw } from 'lucide-react'
import { api, APIError } from '../api'
import { formatUsagePercent, formatUsageResetTime, formatUsageWindow } from '../providerUsage'

const usageRefreshIntervalMS = 60_000

export default function ProviderUsageSection({ providerID }: { providerID: number }) {
  const usage = useQuery({
    queryKey: ['provider-usage', providerID],
    queryFn: () => api.providerUsage(providerID),
    refetchInterval: usageRefreshIntervalMS,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: true,
    retry: (count, error) => !(error instanceof APIError && error.code === 'provider_usage_unsupported') && count < 1,
  })
  const unsupported = usage.error instanceof APIError && usage.error.code === 'provider_usage_unsupported'

  return <section className="form-section settings-section provider-usage-section">
    <div className="section-heading model-catalog-heading"><div><h2>Usage</h2><p>Current provider rate limits. This refreshes every minute while the page is open.</p></div><button type="button" className="button secondary" onClick={() => usage.refetch()} disabled={usage.isFetching}>{usage.isFetching ? <LoaderCircle className="spin" size={16} /> : <RefreshCw size={16} />} Refresh usage</button></div>
    {usage.isLoading ? <div className="usage-grid"><div className="skeleton usage-skeleton" /><div className="skeleton usage-skeleton" /></div> : null}
    {!usage.data && usage.error ? <div className={`inline-alert ${unsupported ? '' : 'warning'}`}><CircleAlert /><div><b>{unsupported ? 'Usage is not available' : 'Could not load usage'}</b><p>{unsupported ? 'This provider does not expose compatible usage data.' : usage.error instanceof APIError ? usage.error.message : 'The provider usage request failed.'}</p></div></div> : null}
    {usage.data ? <>
      <div className="usage-summary"><span><Clock3 /> Updated {new Date(usage.data.fetched_at).toLocaleString()}</span>{usage.isError && <span className="warning-text"><CircleAlert /> Refresh failed; showing the last successful result</span>}</div>
      {usage.data.limits.length ? <div className="usage-grid">{usage.data.limits.map(limit => <article className="usage-limit-card" key={limit.id}>
        <header><div><Gauge /><h3>{limit.name}</h3></div>{limit.limit_reached || !limit.allowed ? <span className="status-pill danger">Limit reached</span> : <span className="status-pill">Available</span>}</header>
        <div className="usage-window-list">{limit.windows.map(window => {
          const progress = Math.max(0, Math.min(100, window.used_percent))
          const level = limit.limit_reached || window.used_percent >= 95 ? 'danger' : window.used_percent >= 80 ? 'warning' : ''
          return <div className={`usage-window ${level}`} key={window.kind}>
            <div className="usage-window-heading"><span>{formatUsageWindow(window)}</span><b>{formatUsagePercent(window.used_percent)}% used</b></div>
            <div className="usage-progress" role="progressbar" aria-label={`${limit.name} ${formatUsageWindow(window)}`} aria-valuemin={0} aria-valuemax={100} aria-valuenow={progress}><span style={{ width: `${progress}%` }} /></div>
            <div className="usage-window-meta"><span>{formatUsagePercent(Math.max(0, 100 - window.used_percent))}% remaining</span>{window.resets_at && <time dateTime={window.resets_at}>Resets {formatUsageResetTime(window.resets_at)}</time>}</div>
          </div>
        })}</div>
      </article>)}</div> : <div className="empty-inline"><Gauge /><div><b>No active usage windows</b><p>The provider returned no percentage-based limits.</p></div></div>}
    </> : null}
  </section>
}
