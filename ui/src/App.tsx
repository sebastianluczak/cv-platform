import { useEffect, useRef, useState } from 'react'
import './App.css'
import Footer from './components/Footer'
import PrivacyPolicy from './components/PrivacyPolicy'
import DataRetentionPolicy from './components/DataRetentionPolicy'
import ConsentCheckbox from './components/ConsentCheckbox'
import MarketDashboard from './MarketDashboard'

type ProgressEvent = {
  job_id: string
  stage: 'queued' | 'processing' | 'retrying' | 'redacting' | 'matching' | 'completed' | 'failed'
  message: string
  attempt: number
  updated_at: string
}

type MatchResult = {
  version: number
  job_id: string
  scored_at: string
  categories: {
    category: string
    score: number
    evidence: string[]
  }[]
}

type OfferMatch = {
  match_id: string
  resume_job_id: string
  status: 'queued' | 'processing' | 'completed' | 'unscorable' | 'failed'
  offer_source: 'text' | 'url'
  message: string
  percent?: number
  required_count?: number
  matched_count?: number
  matched_skills?: string[]
  missing_skills?: string[]
}

type AIWeigh = {
  weigh_id: string
  match_id: string
  resume_job_id: string
  status: 'queued' | 'processing' | 'completed' | 'failed'
  message: string
  markdown?: string
}

type RoleInsights = {
  insight_id: string
  resume_job_id: string
  status: 'queued' | 'processing' | 'completed' | 'failed'
  message: string
  roles?: {
    title: string
    fit_summary: string
    responsibilities: string[]
  }[]
}

const reviewStages = [
  { label: 'Queued', stage: 0 },
  { label: 'Text extraction', stage: 1 },
  { label: 'Privacy filtering', stage: 2 },
  { label: 'Role matching', stage: 3 },
]

const reviewAreas = [
  { title: 'Engineering', categories: 'Backend · Frontend · Mobile · Embedded' },
  { title: 'Systems', categories: 'DevOps · Platform · Architecture · Security' },
  { title: 'Quality & data', categories: 'QA · Data · Support' },
  { title: 'Technology leadership', categories: 'Management · Product · Business' },
]

function currentReviewStage(stage: ProgressEvent['stage']) {
  switch (stage) {
    case 'queued': return 0
    case 'processing':
    case 'retrying': return 1
    case 'redacting': return 2
    case 'matching':
    case 'completed': return 3
    case 'failed': return -1
  }
}

function App() {
  const pageShell = useRef<HTMLElement>(null)
  const fileInput = useRef<HTMLInputElement>(null)
  const eventSource = useRef<EventSource | null>(null)
  const activeJobID = useRef('')
  const requestGeneration = useRef(0)
  const aiRequestGeneration = useRef(0)
  const roleInsightsRequestGeneration = useRef(0)
  const [file, setFile] = useState<File | null>(null)
  const [dragging, setDragging] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [message, setMessage] = useState('')
  const [uploaded, setUploaded] = useState(false)
  const [progress, setProgress] = useState<ProgressEvent | null>(null)
  const [matchResult, setMatchResult] = useState<MatchResult | null>(null)
  const [resultError, setResultError] = useState('')
  const [offerInputMode, setOfferInputMode] = useState<'url' | 'text'>('url')
  const [offerURL, setOfferURL] = useState('')
  const [offerText, setOfferText] = useState('')
  const [offerMatch, setOfferMatch] = useState<OfferMatch | null>(null)
  const [offerMatching, setOfferMatching] = useState(false)
  const [offerError, setOfferError] = useState('')
  const [aiWeigh, setAIWeigh] = useState<AIWeigh | null>(null)
  const [aiGenerating, setAIGenerating] = useState(false)
  const [aiError, setAIError] = useState('')
  const [aiConsent, setAIConsent] = useState(false)
  const [roleInsights, setRoleInsights] = useState<RoleInsights | null>(null)
  const [roleInsightsGenerating, setRoleInsightsGenerating] = useState(false)
  const [roleInsightsError, setRoleInsightsError] = useState('')
  const [roleInsightsConsent, setRoleInsightsConsent] = useState(false)
  const [offerPanelOpen, setOfferPanelOpen] = useState(false)
  const [connection, setConnection] = useState<'connecting' | 'connected' | 'reconnecting' | null>(null)
  const [consentGiven, setConsentGiven] = useState(false)
  const [currentPath, setCurrentPath] = useState(() => {
    const hash = window.location.hash.slice(1)
    return hash || '/'
  })

  useEffect(() => {
    const handleHashChange = () => {
      const hash = window.location.hash.slice(1)
      setCurrentPath(hash || '/')
    }
    window.addEventListener('hashchange', handleHashChange)
    return () => window.removeEventListener('hashchange', handleHashChange)
  }, [])

  useEffect(() => () => eventSource.current?.close(), [])

  function clearAIWeight() {
    aiRequestGeneration.current += 1
    setAIWeigh(null)
    setAIGenerating(false)
    setAIError('')
    setAIConsent(false)
  }

  function clearRoleInsights() {
    roleInsightsRequestGeneration.current += 1
    setRoleInsights(null)
    setRoleInsightsGenerating(false)
    setRoleInsightsError('')
    setRoleInsightsConsent(false)
  }

  function trackCursorTint(event: React.PointerEvent<HTMLElement>) {
    if (event.pointerType !== 'mouse' || !pageShell.current) return
    const bounds = pageShell.current.getBoundingClientRect()
    pageShell.current.style.setProperty('--cursor-x', `${event.clientX - bounds.left}px`)
    pageShell.current.style.setProperty('--cursor-y', `${event.clientY - bounds.top}px`)
    pageShell.current.style.setProperty('--cursor-glow-opacity', '1')
  }

  function clearCursorTint(event: React.PointerEvent<HTMLElement>) {
    if (event.pointerType !== 'mouse' || !pageShell.current) return
    pageShell.current.style.setProperty('--cursor-glow-opacity', '0')
  }

  function chooseFile(nextFile?: File) {
    requestGeneration.current += 1
    activeJobID.current = ''
    eventSource.current?.close()
    eventSource.current = null
    setMessage('')
    setUploaded(false)
    setUploading(false)
    setProgress(null)
    setMatchResult(null)
    setResultError('')
    setOfferURL('')
    setOfferText('')
    setOfferMatch(null)
    setOfferMatching(false)
    setOfferError('')
    clearAIWeight()
    clearRoleInsights()
    setOfferPanelOpen(false)
    setConnection(null)
    if (!nextFile) return
    if (nextFile.size > 10 * 1024 * 1024) {
      setFile(null)
      setMessage('That file is over 10 MB. Choose a smaller resume to continue.')
      return
    }
    setFile(nextFile)
  }

  async function uploadResume() {
    if (!file || !consentGiven) return
    const generation = ++requestGeneration.current
    setUploading(true)
    setMessage('')
    setOfferMatch(null)
    setOfferError('')
    const formData = new FormData()
    formData.append('file', file)

    try {
      const response = await fetch(import.meta.env.VITE_UPLOAD_ENDPOINT || '/api/upload', {
        method: 'POST',
        body: formData,
      })
      if (generation !== requestGeneration.current) return
      if (!response.ok) throw new Error(await response.text())
      const result = await response.json() as { job_id: string }
      if (generation !== requestGeneration.current) return
      if (!result.job_id) throw new Error('Upload response did not include a job ID')

      activeJobID.current = result.job_id
      setUploaded(true)
      setMessage('CV received. Technical review is underway.')
      setConnection('connecting')
      const endpoint = new URL(import.meta.env.VITE_EVENTS_ENDPOINT || '/api/events', window.location.href)
      endpoint.searchParams.set('job_id', result.job_id)
      const source = new EventSource(endpoint)
      eventSource.current = source
      source.onopen = () => setConnection('connected')
      source.onmessage = (event) => {
        try {
          const update = JSON.parse(event.data) as ProgressEvent
          if (activeJobID.current !== result.job_id || update.job_id !== result.job_id) return
          setProgress(update)
          if (update.stage === 'completed' || update.stage === 'failed') {
            source.close()
            setConnection(null)
            if (update.stage === 'completed') {
              void (async () => {
                try {
                  const endpoint = new URL(import.meta.env.VITE_RESULTS_ENDPOINT || '/api/results', window.location.href)
                  endpoint.searchParams.set('job_id', result.job_id)
                  const resultResponse = await fetch(endpoint)
                  if (!resultResponse.ok) throw new Error('Could not load match results')
                  const matchResult = await resultResponse.json() as MatchResult
                  if (activeJobID.current === result.job_id) setMatchResult(matchResult)
                } catch {
                  if (activeJobID.current === result.job_id) {
                    setResultError('Your resume was matched, but the scores could not be loaded.')
                  }
                }
              })()
            }
          }
        } catch {
          setMessage('Received an invalid processing update. Reconnecting…')
        }
      }
      source.onerror = () => setConnection('reconnecting')
    } catch {
    if (generation === requestGeneration.current) {
      setMessage('Upload failed. Check that the resume service is running, then try again.')
    }
    } finally {
    if (generation === requestGeneration.current) setUploading(false)
    }
  }

  async function compareOffer() {
    const resumeJobID = activeJobID.current
    if (!resumeJobID || !matchResult) return
    const offer = offerInputMode === 'url' ? { offer_url: offerURL.trim() } : { offer_text: offerText.trim() }
    if (!Object.values(offer)[0]) {
      setOfferError(offerInputMode === 'url' ? 'Paste a job offer URL to continue.' : 'Paste the job offer text to continue.')
      return
    }
    const generation = requestGeneration.current
    setOfferMatching(true)
    setOfferError('')
    setOfferMatch(null)
    clearAIWeight()
    try {
      const endpoint = new URL(import.meta.env.VITE_OFFER_MATCHER_ENDPOINT || '/offer-api', window.location.href)
      const matchesPath = `${endpoint.pathname.replace(/\/+$/, '')}/matches`
      const response = await fetch(new URL(matchesPath, endpoint.origin), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ resume_job_id: resumeJobID, ...offer }),
      })
      if (!response.ok) throw new Error('Could not start the offer comparison. Try again.')
      const accepted = await response.json() as { match_id?: string }
      if (!accepted.match_id) throw new Error('Offer matcher did not return a match ID.')

      for (let attempt = 0; attempt < 150; attempt++) {
        if (generation !== requestGeneration.current || activeJobID.current !== resumeJobID) return
        const statusEndpoint = new URL(`${matchesPath}/${encodeURIComponent(accepted.match_id)}`, endpoint.origin)
        const statusResponse = await fetch(statusEndpoint)
        if (!statusResponse.ok) throw new Error('Could not load the offer comparison status.')
        const result = await statusResponse.json() as OfferMatch
        if (result.match_id !== accepted.match_id) throw new Error('Received an invalid offer comparison result.')
        setOfferMatch(result)
        if (result.status === 'completed' || result.status === 'unscorable' || result.status === 'failed') return
        await new Promise((resolve) => window.setTimeout(resolve, 2000))
      }
      setOfferError('The comparison is taking longer than expected. You can wait and try again.')
    } catch (error) {
      if (generation === requestGeneration.current) {
        setOfferError(error instanceof Error ? error.message : 'Offer comparison failed. Try again.')
      }
    } finally {
      if (generation === requestGeneration.current) setOfferMatching(false)
    }
  }

  async function generateResume() {
    if (!offerMatch || offerMatch.status !== 'completed' || !aiConsent) return
    const matchID = offerMatch.match_id
    const generation = ++aiRequestGeneration.current
    setAIGenerating(true)
    setAIError('')
    setAIWeigh(null)
    try {
      const endpoint = new URL(import.meta.env.VITE_AIWEIGHTER_ENDPOINT || '/ai-api', window.location.href)
      const weightsPath = `${endpoint.pathname.replace(/\/+$/, '')}/weights`
      const response = await fetch(new URL(weightsPath, endpoint.origin), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ match_id: matchID, consent: true }),
      })
      if (!response.ok) throw new Error('Could not start AI resume generation. Try again.')
      const accepted = await response.json() as { weigh_id?: string }
      if (!accepted.weigh_id) throw new Error('AIweighter did not return a request ID.')

      for (let attempt = 0; attempt < 150; attempt++) {
        if (generation !== aiRequestGeneration.current) return
        const statusEndpoint = new URL(`${weightsPath}/${encodeURIComponent(accepted.weigh_id)}`, endpoint.origin)
        const statusResponse = await fetch(statusEndpoint)
        if (!statusResponse.ok) throw new Error('Could not load AI resume status.')
        const result = await statusResponse.json() as AIWeigh
        if (result.weigh_id !== accepted.weigh_id || result.match_id !== matchID) {
          throw new Error('Received an invalid AI resume result.')
        }
        setAIWeigh(result)
        if (result.status === 'completed' || result.status === 'failed') return
        await new Promise((resolve) => window.setTimeout(resolve, 2000))
      }
      setAIError('Resume generation is taking longer than expected. You can wait and try again.')
    } catch (error) {
      if (generation === aiRequestGeneration.current) {
        setAIError(error instanceof Error ? error.message : 'AI resume generation failed. Try again.')
      }
    } finally {
      if (generation === aiRequestGeneration.current) setAIGenerating(false)
    }
  }

  async function generateRoleInsights() {
    const resumeJobID = activeJobID.current
    if (!resumeJobID || !matchResult || !roleInsightsConsent) return
    const generation = ++roleInsightsRequestGeneration.current
    setRoleInsightsGenerating(true)
    setRoleInsightsError('')
    setRoleInsights(null)
    try {
      const endpoint = new URL(import.meta.env.VITE_ROLEINSIGHTS_ENDPOINT || '/insights-api', window.location.href)
      const insightsPath = `${endpoint.pathname.replace(/\/+$/, '')}/insights`
      const response = await fetch(new URL(insightsPath, endpoint.origin), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ resume_job_id: resumeJobID, consent: true }),
      })
      if (!response.ok) throw new Error('Could not start role insights. Try again.')
      const accepted = await response.json() as { insight_id?: string }
      if (!accepted.insight_id) throw new Error('Role insights service did not return a request ID.')

      for (let attempt = 0; attempt < 150; attempt++) {
        if (generation !== roleInsightsRequestGeneration.current || activeJobID.current !== resumeJobID) return
        const statusEndpoint = new URL(`${insightsPath}/${encodeURIComponent(accepted.insight_id)}`, endpoint.origin)
        const statusResponse = await fetch(statusEndpoint)
        if (!statusResponse.ok) throw new Error('Could not load role insights status.')
        const result = await statusResponse.json() as RoleInsights
        if (result.insight_id !== accepted.insight_id || result.resume_job_id !== resumeJobID) {
          throw new Error('Received an invalid role insights result.')
        }
        setRoleInsights(result)
        if (result.status === 'completed' || result.status === 'failed') return
        await new Promise((resolve) => window.setTimeout(resolve, 2000))
      }
      setRoleInsightsError('Role insights are taking longer than expected. You can wait and try again.')
    } catch (error) {
      if (generation === roleInsightsRequestGeneration.current) {
        setRoleInsightsError(error instanceof Error ? error.message : 'Role insights failed. Try again.')
      }
    } finally {
      if (generation === roleInsightsRequestGeneration.current) setRoleInsightsGenerating(false)
    }
  }

  function downloadResumeMarkdown() {
    if (!aiWeigh?.markdown) return
    const blob = new Blob([aiWeigh.markdown], { type: 'text/markdown;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `tailored-resume-${aiWeigh.weigh_id}.md`
    link.click()
    window.setTimeout(() => URL.revokeObjectURL(url), 0)
  }

  if (currentPath === '/market') return <MarketDashboard />

  return (
    <main
      className="page-shell"
      ref={pageShell}
      onPointerMove={trackCursorTint}
      onPointerLeave={clearCursorTint}
    >
      {currentPath === '/privacy' ? (
        <PrivacyPolicy />
      ) : currentPath === '/data-retention' ? (
        <DataRetentionPolicy />
      ) : (
        <>
          <header className="topbar">
            <a className="brand" href="#/" aria-label="Draftline home">
              <span className="brand-mark">d.</span>
              <span>draftline</span>
            </a>
            <div className="topbar-links">
              <a href="#/market">Market demand</a>
              <span className="topbar-note">Technical experience, in focus</span>
            </div>
          </header>

          <section className="hero-section" id="top">
            <div className="hero-copy">
              <p className="eyebrow"><span /> A TECHNICAL CV REVIEW</p>
              <h1>Make your experience <em>work in context.</em></h1>
              <p className="intro">
                Review technical signals, explore broader roles suggested from your CV, and compare with a job description when you are ready.
              </p>
            </div>

            <div className="review-workspace">
          <section className="workspace-panel upload-panel" aria-label="CV upload and processing">
            <div className="panel-heading">
              <span className="panel-kicker">01 / INPUT</span>
              <h2>Start with your CV</h2>
            </div>
            <label
              className={`dropzone${dragging ? ' is-dragging' : ''}`}
              onDragOver={(event) => {
                event.preventDefault()
                setDragging(true)
              }}
              onDragLeave={() => setDragging(false)}
              onDrop={(event) => {
                event.preventDefault()
                setDragging(false)
                chooseFile(event.dataTransfer.files[0])
              }}
            >
              <input
                ref={fileInput}
                type="file"
                accept=".pdf,.png,.jpg,.jpeg,.tif,.tiff,.bmp"
                onChange={(event) => chooseFile(event.target.files?.[0])}
              />
              <span className="upload-symbol" aria-hidden="true">↑</span>
              <span className="dropzone-title">{file ? file.name : 'Drop your CV here'}</span>
              <span className="dropzone-caption">
                {file ? `${(file.size / (1024 * 1024)).toFixed(1)} MB · Select to change` : 'Click to browse · PDF or image · up to 10 MB'}
              </span>
            </label>
            <ConsentCheckbox checked={consentGiven} onChange={setConsentGiven} />
            <button
              className="upload-button"
              type="button"
              disabled={!file || uploading || uploaded || !consentGiven}
              title={!consentGiven ? 'Please review and accept the privacy and data retention policies' : ''}
              onClick={uploadResume}
            >
              {uploading ? 'Uploading…' : uploaded ? 'CV received' : 'Review my CV'}
              {!uploading && !uploaded && <span aria-hidden="true">↗</span>}
            </button>

            {(progress || connection) && (
              <section className={`progress-panel${progress?.stage === 'failed' ? ' has-failed' : ''}`} aria-live="polite" aria-label="CV review progress">
                <div className="progress-heading">
                  <strong>{progress?.stage === 'completed' ? 'Technical review complete' : progress?.stage === 'failed' ? 'Review needs attention' : 'Review in progress'}</strong>
                  {connection && <span>{connection === 'reconnecting' ? 'Reconnecting…' : connection === 'connecting' ? 'Connecting…' : 'Live'}</span>}
                </div>
                {progress && (
                  <>
                    <ol className="progress-steps">
                      {reviewStages.map((step, index) => {
                        const activeStage = currentReviewStage(progress.stage)
                        const state = progress.stage === 'completed' || index < activeStage
                          ? 'is-complete'
                          : index === activeStage && progress.stage !== 'failed'
                            ? 'is-active'
                            : ''
                        return (
                          <li className={state} key={step.label}>
                            <span aria-hidden="true">{state === 'is-complete' ? '✓' : `0${index + 1}`}</span>
                            <small>{step.label}</small>
                          </li>
                        )
                      })}
                    </ol>
                    <p className={`progress-message${progress.stage === 'failed' ? ' is-error' : ''}`}>
                      {progress.stage === 'retrying' ? `Retrying · attempt ${progress.attempt}` : progress.message}
                    </p>
                  </>
                )}
              </section>
            )}
            {resultError && <p className="result-error" role="alert">{resultError}</p>}
            <p className={`upload-message${message && !uploaded ? ' is-error' : ''}`} aria-live="polite">
              {message || 'Your CV is processed for this review and is not used to assess your ability.'}
            </p>
          </section>

          <section className="workspace-panel results-panel" aria-label="Resume role insights and technical signals">
            {matchResult ? (
              <>
                <div className="results-heading">
                  <div>
                    <span className="panel-kicker">02 / REVIEW</span>
                    <h2>Technical domain signals</h2>
                  </div>
                  <span className="score-legend">Pattern matches · each area scored 1–5</span>
                </div>
                <ul className="match-grid">
                  {matchResult.categories.map((category) => (
                    <li key={category.category}>
                      <details className="category-detail">
                        <summary>
                          <span className="match-category-name">{category.category}</span>
                          <span className="match-score">{category.score}<small>/5</small></span>
                        </summary>
                        <p>{category.evidence.length > 0 ? category.evidence.join(', ') : 'No matching patterns found in the CV.'}</p>
                      </details>
                    </li>
                  ))}
                </ul>
                <p className="results-note">Scores reflect matching terms in the CV, not demonstrated proficiency or role suitability. Select a domain to see detected terms.</p>

                <section className="role-insights-panel" aria-label="Explore roles suggested from your resume">
                  <span className="panel-kicker">OPTIONAL / AI-ASSISTED</span>
                  <h3>Explore roles beyond technical scores</h3>
                  <p id="role-insights-disclosure">
                    Get role ideas across different kinds of work, based on your redacted CV rather than your technical scores.
                    Your redacted CV will be sent to OpenRouter, an external AI service. Redaction is best-effort; suggestions
                    are advisory and should be reviewed.
                  </p>
                  {!roleInsights && (
                    <>
                      <label className="ai-consent">
                        <input
                          type="checkbox"
                          checked={roleInsightsConsent}
                          disabled={roleInsightsGenerating}
                          aria-describedby="role-insights-disclosure"
                          onChange={(event) => setRoleInsightsConsent(event.target.checked)}
                        />
                        <span>I agree to send my redacted CV to the external AI service for role suggestions.</span>
                      </label>
                      <button
                        className="compare-button ai-generate-button"
                        type="button"
                        disabled={!roleInsightsConsent || roleInsightsGenerating}
                        onClick={() => void generateRoleInsights()}
                      >
                        {roleInsightsGenerating ? 'Finding role ideas…' : 'Suggest roles for my CV'}
                      </button>
                    </>
                  )}
                  {roleInsightsError && <p className="result-error" role="alert">{roleInsightsError}</p>}
                  {roleInsights && (
                    <div className="role-insights-result" aria-live="polite">
                      <p className={`offer-match-status${roleInsights.status === 'failed' ? ' is-error' : ''}`}>
                        {roleInsights.message}
                        {(roleInsights.status === 'queued' || roleInsights.status === 'processing') && ' · Working'}
                      </p>
                      {roleInsights.status === 'completed' && roleInsights.roles?.map((role) => (
                        <article className="role-insight" key={role.title}>
                          <h4>{role.title}</h4>
                          <p>{role.fit_summary}</p>
                          <strong>Typical responsibilities</strong>
                          <ul>
                            {role.responsibilities.map((responsibility) => <li key={responsibility}>{responsibility}</li>)}
                          </ul>
                        </article>
                      ))}
                      {(roleInsights.status === 'failed' || roleInsightsError) && (
                        <button
                          className="ai-download-button"
                          type="button"
                          onClick={clearRoleInsights}
                        >
                          Try again
                        </button>
                      )}
                    </div>
                  )}
                </section>

                <section className={`offer-panel${offerPanelOpen ? ' is-open' : ''}`} aria-label="Compare with a job description">
                  <button
                    className="offer-toggle"
                    type="button"
                    aria-expanded={offerPanelOpen}
                    aria-controls="offer-comparison-form"
                    onClick={() => setOfferPanelOpen((open) => !open)}
                  >
                    <span>
                      <strong>Compare with a job description</strong>
                      <small>See which required skills appear in your CV</small>
                    </span>
                    <span className="offer-toggle-icon" aria-hidden="true">{offerPanelOpen ? '−' : '+'}</span>
                  </button>
                  {offerPanelOpen && (
                    <div className="offer-form" id="offer-comparison-form">
                      <div className="offer-input-tabs" role="group" aria-label="Job description input type">
                        <button type="button" aria-pressed={offerInputMode === 'url'} disabled={offerMatching || aiGenerating} onClick={() => { setOfferInputMode('url'); setOfferMatch(null); setOfferError(''); clearAIWeight() }}>Job URL</button>
                        <button type="button" aria-pressed={offerInputMode === 'text'} disabled={offerMatching || aiGenerating} onClick={() => { setOfferInputMode('text'); setOfferMatch(null); setOfferError(''); clearAIWeight() }}>Paste text</button>
                      </div>
                      {offerInputMode === 'url' ? (
                        <label className="offer-input-label">
                          Job description URL
                          <input
                            type="url"
                            value={offerURL}
                            disabled={offerMatching || aiGenerating}
                            onChange={(event) => { setOfferURL(event.target.value); setOfferMatch(null); setOfferError(''); clearAIWeight() }}
                            placeholder="https://company.example/jobs/role"
                          />
                        </label>
                      ) : (
                        <label className="offer-input-label">
                          Job description
                          <textarea
                            value={offerText}
                            disabled={offerMatching || aiGenerating}
                            onChange={(event) => { setOfferText(event.target.value); setOfferMatch(null); setOfferError(''); clearAIWeight() }}
                            rows={4}
                            maxLength={100 * 1024}
                            placeholder="Paste the job description"
                          />
                        </label>
                      )}
                      <small className="offer-retention-note">The job description is retained with this private match result. It is sent to OpenRouter only if you separately request resume generation and consent.</small>
                      <button className="compare-button" type="button" disabled={offerMatching || aiGenerating} onClick={() => void compareOffer()}>
                        {offerMatching ? 'Comparing…' : 'Compare skill coverage'}
                      </button>
                      {offerError && <p className="result-error" role="alert">{offerError}</p>}
                      {offerMatch && (
                        <div className="offer-match-result" aria-live="polite">
                          <p className={`offer-match-status${offerMatch.status === 'failed' || offerMatch.status === 'unscorable' ? ' is-error' : ''}`}>
                            {offerMatch.status === 'completed' ? `${offerMatch.percent}% identified-skill coverage` : offerMatch.message}
                            {(offerMatch.status === 'queued' || offerMatch.status === 'processing') && ' · Working'}
                          </p>
                          {offerMatch.status === 'completed' && (
                            <>
                              <p className="offer-match-count">{offerMatch.matched_count} of {offerMatch.required_count} identified requirements have matching terms in the CV.</p>
                              <dl>
                                <dt>Detected</dt>
                                <dd>{offerMatch.matched_skills?.length ? offerMatch.matched_skills.join(', ') : 'No matching terms found'}</dd>
                                <dt>Not detected</dt>
                                <dd>{offerMatch.missing_skills?.length ? offerMatch.missing_skills.join(', ') : 'None'}</dd>
                              </dl>
                              <small>Keyword coverage from curated patterns; not an assessment of capability or fit.</small>
                              <section className="ai-resume-panel" aria-label="Prepare a tailored resume">
                                <span className="panel-kicker">OPTIONAL / AI-ASSISTED</span>
                                <h3>Prepare a tailored resume</h3>
                                <p id="ai-resume-disclosure">
                                  Your redacted CV, detected skill evidence, and this job description will be sent to
                                  OpenRouter, an external AI service. Redaction is best-effort. The generated resume
                                  should use only supported facts; review it carefully before use.
                                </p>
                                <label className="ai-consent">
                                  <input
                                    type="checkbox"
                                    checked={aiConsent}
                                    disabled={aiGenerating}
                                    aria-describedby="ai-resume-disclosure"
                                    onChange={(event) => setAIConsent(event.target.checked)}
                                  />
                                  <span>I agree to send this information to the external AI service.</span>
                                </label>
                                <button
                                  className="compare-button ai-generate-button"
                                  type="button"
                                  disabled={!aiConsent || aiGenerating}
                                  onClick={() => void generateResume()}
                                >
                                  {aiGenerating ? 'Preparing resume…' : 'Generate tailored resume'}
                                </button>
                                {aiError && <p className="result-error" role="alert">{aiError}</p>}
                                {aiWeigh && (
                                  <div className="ai-resume-result" aria-live="polite">
                                    <p className={`offer-match-status${aiWeigh.status === 'failed' ? ' is-error' : ''}`}>
                                      {aiWeigh.message}
                                      {(aiWeigh.status === 'queued' || aiWeigh.status === 'processing') && ' · Working'}
                                    </p>
                                    {aiWeigh.status === 'completed' && aiWeigh.markdown && (
                                      <>
                                        <button
                                          className="ai-download-button"
                                          type="button"
                                          onClick={downloadResumeMarkdown}
                                        >
                                          Download Markdown
                                        </button>
                                        <pre>{aiWeigh.markdown}</pre>
                                      </>
                                    )}
                                  </div>
                                )}
                              </section>
                            </>
                          )}
                        </div>
                      )}
                    </div>
                  )}
                </section>
              </>
            ) : (
              <>
                <div className="results-heading">
                  <div>
                    <span className="panel-kicker">02 / REVIEW</span>
                    <h2>Technical experience, in context</h2>
                  </div>
                  <span className="score-legend">14 technical domains</span>
                </div>
                <p className="results-intro">A focused scan for experience signals across software delivery, systems, and technology leadership.</p>
                <div className="review-areas">
                  {reviewAreas.map((area) => (
                    <div className="review-area" key={area.title}>
                      <strong>{area.title}</strong>
                      <span>{area.categories}</span>
                    </div>
                  ))}
                </div>
                <p className="results-note">This is a pattern-based review of CV content, not a measure of capability or seniority. Your identity details are removed before role matching.</p>
              </>
            )}
          </section>
        </div>
      </section>

      <Footer />
        </>
      )}
    </main>
  )
}

export default App
