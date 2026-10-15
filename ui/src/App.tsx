import { useEffect, useRef, useState } from 'react'
import './App.css'

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

function App() {
  const fileInput = useRef<HTMLInputElement>(null)
  const eventSource = useRef<EventSource | null>(null)
  const activeJobID = useRef('')
  const requestGeneration = useRef(0)
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
  const [connection, setConnection] = useState<'connecting' | 'connected' | 'reconnecting' | null>(null)

  useEffect(() => () => eventSource.current?.close(), [])

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
    if (!file) return
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
      setMessage('Resume received. Follow its progress below.')
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

  return (
    <main className="page-shell">
      <header className="topbar">
        <a className="brand" href="#top" aria-label="Draftline home">
          <span className="brand-mark">d.</span>
          <span>draftline</span>
        </a>
        <span className="topbar-note">A clearer next step</span>
      </header>

      <section className="hero-section" id="top">
        <div className="hero-copy">
          <p className="eyebrow"><span /> YOUR RESUME, WITH DIRECTION</p>
          <h1>Make your next move <em>make sense.</em></h1>
          <p className="intro">
            Upload your resume and get a clearer picture of what is working,
            what is missing, and where to go next.
          </p>

          <div className={`upload-panel${uploaded ? ' is-uploaded' : ''}`}>
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
              <span className="dropzone-title">
                {file ? file.name : 'Drop your resume here'}
              </span>
              <span className="dropzone-caption">
                {file ? `${(file.size / (1024 * 1024)).toFixed(1)} MB · Click to change` : 'or click to browse · PDF or image · up to 10 MB'}
              </span>
            </label>
            <button
              className="upload-button"
              type="button"
              disabled={!file || uploading || uploaded}
              onClick={uploadResume}
            >
              {uploading ? 'Uploading…' : uploaded ? 'Resume received' : 'Get resume feedback'}
              {!uploading && !uploaded && <span aria-hidden="true">↗</span>}
            </button>
            {progress && (
              <section className="progress-panel" aria-live="polite" aria-label="Resume processing progress">
                <div className="progress-heading">
                  <strong>Processing your resume</strong>
                  {connection && <span>{connection === 'reconnecting' ? 'Reconnecting…' : connection === 'connecting' ? 'Connecting…' : 'Live'}</span>}
                </div>
                <ol className="progress-steps">
                  <li className="is-complete"><span>01</span><div><strong>Queued</strong><small>{progress.stage === 'queued' ? progress.message : 'Your resume is safely in line.'}</small></div></li>
                  <li className={progress.stage === 'processing' || progress.stage === 'retrying' ? 'is-active' : progress.stage === 'redacting' || progress.stage === 'matching' || progress.stage === 'completed' ? 'is-complete' : progress.stage === 'failed' ? 'is-failed' : ''}>
                    <span>02</span>
                    <div>
                      <strong>{progress.stage === 'retrying' ? 'Retrying processing' : 'Extracting text'}</strong>
                      <small>{progress.stage === 'queued' ? 'Waiting for extraction to start.' : progress.message}{progress.stage === 'retrying' && progress.attempt > 0 ? ` (attempt ${progress.attempt})` : ''}</small>
                    </div>
                  </li>
                  <li className={progress.stage === 'redacting' ? 'is-active' : progress.stage === 'matching' || progress.stage === 'completed' ? 'is-complete' : progress.stage === 'failed' ? 'is-failed' : ''}><span>03</span><div><strong>{progress.stage === 'failed' ? 'Needs attention' : 'Removing personal identifiers'}</strong><small>{progress.stage === 'failed' ? 'We could not prepare the redacted resume.' : progress.stage === 'redacting' ? progress.message : progress.stage === 'matching' || progress.stage === 'completed' ? 'Personal identifiers were removed from the resume.' : 'Waiting for text extraction to finish.'}</small></div></li>
                  <li className={progress.stage === 'matching' ? 'is-active' : progress.stage === 'completed' ? 'is-complete' : progress.stage === 'failed' ? 'is-failed' : ''}><span>04</span><div><strong>{progress.stage === 'failed' ? 'Needs attention' : progress.stage === 'completed' ? 'Role scores ready' : 'Matching role profiles'}</strong><small>{progress.stage === 'failed' ? 'We could not finish the resume review.' : progress.stage === 'matching' ? progress.message : progress.stage === 'completed' ? 'Your role profile scores are ready.' : 'Waiting for redaction to finish.'}</small></div></li>
                </ol>
              </section>
            )}
            {resultError && <p className="result-error" role="alert">{resultError}</p>}
            {matchResult && (
              <section className="match-results" aria-label="Role profile scores">
                <div className="match-results-heading">
                  <strong>Your role profile scores</strong>
                  <span>Heuristic match · out of 5</span>
                </div>
                <ul>
                  {matchResult.categories.map((category) => (
                    <li key={category.category}>
                      <span className="match-category-name">{category.category}</span>
                      <span className="match-score">{category.score}/5</span>
                      <small>{category.evidence.length > 0 ? category.evidence.join(', ') : 'No matching patterns found'}</small>
                    </li>
                  ))}
                </ul>
              </section>
            )}
            {matchResult && (
              <section className="offer-panel" aria-label="Compare resume with a job offer">
                <div className="match-results-heading">
                  <strong>Compare with a job offer</strong>
                  <span>Using your completed resume</span>
                </div>
                <div className="offer-input-tabs" role="group" aria-label="Offer input type">
                  <button type="button" aria-pressed={offerInputMode === 'url'} disabled={offerMatching} onClick={() => { setOfferInputMode('url'); setOfferMatch(null); setOfferError('') }}>Offer URL</button>
                  <button type="button" aria-pressed={offerInputMode === 'text'} disabled={offerMatching} onClick={() => { setOfferInputMode('text'); setOfferMatch(null); setOfferError('') }}>Paste text</button>
                </div>
                {offerInputMode === 'url' ? (
                  <label className="offer-input-label">
                    Job offer URL
                    <input
                      type="url"
                      value={offerURL}
                      disabled={offerMatching}
                      onChange={(event) => { setOfferURL(event.target.value); setOfferMatch(null); setOfferError('') }}
                      placeholder="https://company.example/jobs/role"
                    />
                  </label>
                ) : (
                  <label className="offer-input-label">
                    Job offer text
                    <textarea
                      value={offerText}
                      disabled={offerMatching}
                      onChange={(event) => { setOfferText(event.target.value); setOfferMatch(null); setOfferError('') }}
                      rows={5}
                      maxLength={100 * 1024}
                      placeholder="Paste the job description here"
                    />
                  </label>
                )}
                <button className="upload-button" type="button" disabled={offerMatching} onClick={() => void compareOffer()}>
                  {offerMatching ? 'Comparing…' : 'Compare required skills'}
                  {!offerMatching && <span aria-hidden="true">↗</span>}
                </button>
                {offerError && <p className="result-error" role="alert">{offerError}</p>}
                {offerMatch && (
                  <div className="offer-match-result" aria-live="polite">
                    <p className={`offer-match-status${offerMatch.status === 'failed' ? ' is-error' : ''}`}>
                      {offerMatch.status === 'completed' ? `${offerMatch.percent}% required-skill coverage` : offerMatch.message}
                      {(offerMatch.status === 'queued' || offerMatch.status === 'processing') && ' · Working'}
                    </p>
                    {offerMatch.status === 'completed' && (
                      <>
                        <p className="offer-match-count">{offerMatch.matched_count} of {offerMatch.required_count} identified required skills found in your resume.</p>
                        <dl>
                          <dt>Matched</dt>
                          <dd>{offerMatch.matched_skills?.length ? offerMatch.matched_skills.join(', ') : 'No matching signals found'}</dd>
                          <dt>Not found</dt>
                          <dd>{offerMatch.missing_skills?.length ? offerMatch.missing_skills.join(', ') : 'None'}</dd>
                        </dl>
                        <small>Deterministic keyword coverage from curated skill patterns; it is not an assessment of ability or suitability.</small>
                      </>
                    )}
                  </div>
                )}
              </section>
            )}
            <p className={`upload-message${message && !uploaded ? ' is-error' : ''}`} aria-live="polite">
              {message || 'Your resume stays yours. We only use it to prepare your review.'}
            </p>
          </div>
          <div className="proof-line">
            <span className="proof-dot" />
            <span>Built for the job you want, not just the one you have.</span>
          </div>
        </div>

        <aside className="preview" aria-label="Resume review areas">
          <div className="preview-topline">
            <span>THE REVIEW</span>
            <span className="preview-index">01 / 03</span>
          </div>
          <div className="preview-heading">
            <span className="preview-icon" aria-hidden="true">✳</span>
            <div>
              <p>START WITH THE STORY</p>
              <h2>A resume should sound like you.</h2>
            </div>
          </div>
          <div className="review-list">
            <div className="review-row">
              <span className="row-number">01</span>
              <span className="row-copy"><strong>Make it clear</strong><small>Structure and readability</small></span>
              <span className="row-arrow" aria-hidden="true">↗</span>
            </div>
            <div className="review-row">
              <span className="row-number">02</span>
              <span className="row-copy"><strong>Show your strengths</strong><small>Skills and experience</small></span>
              <span className="row-arrow" aria-hidden="true">↗</span>
            </div>
            <div className="review-row">
              <span className="row-number">03</span>
              <span className="row-copy"><strong>Find your next step</strong><small>Focused, useful feedback</small></span>
              <span className="row-arrow" aria-hidden="true">↗</span>
            </div>
          </div>
          <div className="preview-footer">
            <span className="footer-spark" aria-hidden="true">✳</span>
            <span>Thoughtful feedback. No career-bot clichés.</span>
          </div>
        </aside>
      </section>

      <footer className="page-footer">
        <span>draftline <span className="footer-divider">/</span> Resume feedback, with a point of view.</span>
        <span>PDF & IMAGES · MAX 10 MB</span>
      </footer>
    </main>
  )
}

export default App
