import { useEffect, useState } from 'react'
import './MarketDashboard.css'

type DailyPoint = {
  date: string
  sample_size: number
  matching_count: number
  prevalence: number
}

type SkillTrend = {
  skill_id: string
  current_count: number
  current_prevalence: number
  previous_count: number
  previous_prevalence: number
  change_percentage_points?: number
  relative_change_percent?: number
  daily: DailyPoint[]
}

type MarketSummary = {
  market: string
  role_category?: string
  methodology_version: number
  generated_at: string
  recent_period: { from: string; to: string }
  previous_period: { from: string; to: string }
  recent_sample_size: number
  previous_sample_size: number
  recent_sources: { source_id: string; offer_count: number }[]
  previous_sources: { source_id: string; offer_count: number }[]
  last_observed_at?: string
  warnings: string[]
  skills: SkillTrend[]
}

const roleOptions = [
  ['all', 'All IT roles'],
  ['backend', 'Backend'],
  ['frontend', 'Frontend'],
  ['mobile', 'Mobile'],
  ['platform', 'Platform'],
  ['devops', 'DevOps'],
  ['data', 'Data'],
  ['security', 'Security'],
  ['qa', 'QA'],
  ['architecture', 'Architecture'],
]

function endpointURL(days: number, role: string) {
  const base = import.meta.env.VITE_MARKETINSIGHTS_ENDPOINT || '/market-api'
  const endpoint = new URL(`${base.replace(/\/+$/, '')}/api/market/trends`, window.location.href)
  endpoint.searchParams.set('days', String(days))
  if (role !== 'all') endpoint.searchParams.set('role', role)
  return endpoint
}

function formatDate(value: string) {
  return new Date(value).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric', timeZone: 'UTC' })
}

function SkillSparkline({ skill }: { skill: SkillTrend }) {
  const max = Math.max(1, ...skill.daily.map((point) => point.prevalence))
  const points = skill.daily.map((point, index) => {
    const x = skill.daily.length < 2 ? 50 : index * 100 / (skill.daily.length - 1)
    const y = 30 - point.prevalence / max * 25
    return `${x},${y}`
  }).join(' ')
  return (
    <svg className="market-sparkline" viewBox="0 0 100 32" role="img" aria-label={`${skill.skill_id} prevalence over the recent period`}>
      <polyline points={points} fill="none" stroke="currentColor" strokeWidth="2" vectorEffect="non-scaling-stroke" />
    </svg>
  )
}

function MarketDashboard() {
  const [days, setDays] = useState(30)
  const [role, setRole] = useState('all')
  const [summary, setSummary] = useState<MarketSummary | null>(null)
  const [error, setError] = useState('')
  const loading = summary === null && error === ''

  useEffect(() => {
    const controller = new AbortController()
    fetch(endpointURL(days, role), { signal: controller.signal })
      .then(async (response) => {
        if (!response.ok) throw new Error('Market insights are unavailable. Check that the service is running.')
        return await response.json() as MarketSummary
      })
      .then(setSummary)
      .catch((cause: unknown) => {
        if (!controller.signal.aborted) {
          setError(cause instanceof Error ? cause.message : 'Could not load market insights.')
        }
      })
    return () => controller.abort()
  }, [days, role])

  const hasData = (summary?.recent_sample_size ?? 0) > 0

  return (
    <main className="market-shell">
      <header className="market-topbar">
        <a className="market-brand" href="#/">draftline</a>
        <nav aria-label="Main navigation">
          <a href="#/">CV review</a>
          <a href="#/market" aria-current="page">Market demand</a>
        </nav>
      </header>
      <section className="market-content">
        <p className="market-eyebrow">MARKET DEMAND OBSERVATORY · POLAND IT</p>
        <h1>See what employers are asking for.</h1>
        <p className="market-intro">
          Skill trends from observed job advertisements, with the sample size and source coverage behind every figure.
          These observations describe the collected sources, not the whole labor market.
        </p>

        <div className="market-controls">
          <label>
            Observation window
            <select value={days} onChange={(event) => {
              setSummary(null)
              setError('')
              setDays(Number(event.target.value))
            }}>
              <option value={7}>7 days</option>
              <option value={14}>14 days</option>
              <option value={30}>30 days</option>
              <option value={60}>60 days</option>
              <option value={90}>90 days</option>
            </select>
          </label>
          <label>
            Role category
            <select value={role} onChange={(event) => {
              setSummary(null)
              setError('')
              setRole(event.target.value)
            }}>
              {roleOptions.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
            </select>
          </label>
        </div>

        {loading && <p className="market-state" role="status">Loading market observations…</p>}
        {error && <p className="market-error" role="alert">{error}</p>}
        {!loading && !error && summary && (
          <>
            <section className="market-metrics" aria-label="Market sample context">
              <article>
                <span>Observed advertisements</span>
                <strong>{summary.recent_sample_size.toLocaleString()}</strong>
                <small>{formatDate(summary.recent_period.from)} – {formatDate(summary.recent_period.to)}</small>
              </article>
              <article>
                <span>Contributing sources</span>
                <strong>{summary.recent_sources.length}</strong>
                <small>{summary.recent_sources.length
                  ? summary.recent_sources.map((source) => `${source.source_id} (${source.offer_count})`).join(', ')
                  : 'No source data'}</small>
              </article>
              <article>
                <span>Data freshness</span>
                <strong>{summary.last_observed_at ? formatDate(summary.last_observed_at) : '—'}</strong>
                <small>Latest observation in this view</small>
              </article>
            </section>
            {summary.warnings.length > 0 && (
              <ul className="market-warnings" aria-label="Interpretation warnings">
                {summary.warnings.map((warning) => <li key={warning}>{warning}</li>)}
              </ul>
            )}

            {!hasData ? (
              <section className="market-empty">
                <h2>Market data is not connected yet</h2>
                <p>
                  No real advertisements are loaded. The dashboard will show trends after an authorized source
                  integration imports observations; sample figures are never fabricated.
                </p>
              </section>
            ) : (
              <section className="market-skills">
                <div className="market-section-heading">
                  <div>
                    <p className="market-eyebrow">OBSERVED SKILL PREVALENCE</p>
                    <h2>Skills appearing in job ads</h2>
                  </div>
                  <span>Compared with the previous {days}-day period</span>
                </div>
                <div className="market-table-wrap">
                  <table>
                    <thead>
                      <tr>
                        <th scope="col">Skill</th>
                        <th scope="col">Share of ads</th>
                        <th scope="col">Trend</th>
                        <th scope="col">Change</th>
                        <th scope="col">Evidence</th>
                      </tr>
                    </thead>
                    <tbody>
                      {summary.skills.map((skill) => (
                        <tr key={skill.skill_id}>
                          <th scope="row">{skill.skill_id}</th>
                          <td>
                            <strong>{skill.current_prevalence}%</strong>
                            <span className="market-count">{skill.current_count} of {summary.recent_sample_size} ads</span>
                          </td>
                          <td><SkillSparkline skill={skill} /></td>
                          <td className={skill.change_percentage_points === undefined ? '' : skill.change_percentage_points >= 0 ? 'market-up' : 'market-down'}>
                            {skill.change_percentage_points === undefined
                              ? '—'
                              : `${skill.change_percentage_points > 0 ? '+' : ''}${skill.change_percentage_points} pp`}
                          </td>
                          <td>{skill.current_count.toLocaleString()} advertisements</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                <p className="market-methodology">
                  Prevalence uses distinct advertisements observed at least once in each period; percentage-point
                  change is the primary comparison. Ads are deduplicated within a source, not across sources.
                  Skill matching uses a curated technology allowlist (methodology v{summary.methodology_version});
                  it can miss skills not covered by the vocabulary. Previous-period sample: {summary.previous_sample_size.toLocaleString()} ads
                  {summary.previous_sources.length > 0 && ` across ${summary.previous_sources.length} sources`}.
                </p>
              </section>
            )}
          </>
        )}
      </section>
      <footer className="market-footer">
        <a href="#/">Return to your CV review</a>
        <span>Evidence before inference · Market {summary?.market ?? 'PL-IT'}</span>
      </footer>
    </main>
  )
}

export default MarketDashboard
