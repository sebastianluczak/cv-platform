import { useEffect, useRef, useState } from 'react'
import './App.css'

type ProgressEvent = {
  job_id: string
  stage: 'queued' | 'processing' | 'retrying' | 'redacting' | 'completed' | 'failed'
  message: string
  attempt: number
  updated_at: string
}

function App() {
  const fileInput = useRef<HTMLInputElement>(null)
  const eventSource = useRef<EventSource | null>(null)
  const [file, setFile] = useState<File | null>(null)
  const [dragging, setDragging] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [message, setMessage] = useState('')
  const [uploaded, setUploaded] = useState(false)
  const [progress, setProgress] = useState<ProgressEvent | null>(null)
  const [connection, setConnection] = useState<'connecting' | 'connected' | 'reconnecting' | null>(null)

  useEffect(() => () => eventSource.current?.close(), [])

  function chooseFile(nextFile?: File) {
    eventSource.current?.close()
    eventSource.current = null
    setMessage('')
    setUploaded(false)
    setProgress(null)
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
    setUploading(true)
    setMessage('')
    const formData = new FormData()
    formData.append('file', file)

    try {
      const response = await fetch(import.meta.env.VITE_UPLOAD_ENDPOINT || '/api/upload', {
        method: 'POST',
        body: formData,
      })
      if (!response.ok) throw new Error(await response.text())
      const result = await response.json() as { job_id: string }
      if (!result.job_id) throw new Error('Upload response did not include a job ID')

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
          if (update.job_id !== result.job_id) return
          setProgress(update)
          if (update.stage === 'completed' || update.stage === 'failed') {
            source.close()
            setConnection(null)
          }
        } catch {
          setMessage('Received an invalid processing update. Reconnecting…')
        }
      }
      source.onerror = () => setConnection('reconnecting')
    } catch {
      setMessage('Upload failed. Check that the resume service is running, then try again.')
    } finally {
      setUploading(false)
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
                  <li className={progress.stage === 'processing' || progress.stage === 'retrying' ? 'is-active' : progress.stage === 'redacting' || progress.stage === 'completed' ? 'is-complete' : progress.stage === 'failed' ? 'is-failed' : ''}>
                    <span>02</span>
                    <div>
                      <strong>{progress.stage === 'retrying' ? 'Retrying processing' : 'Extracting text'}</strong>
                      <small>{progress.stage === 'queued' ? 'Waiting for extraction to start.' : progress.message}{progress.stage === 'retrying' && progress.attempt > 0 ? ` (attempt ${progress.attempt})` : ''}</small>
                    </div>
                  </li>
                  <li className={progress.stage === 'redacting' ? 'is-active' : progress.stage === 'completed' ? 'is-complete' : progress.stage === 'failed' ? 'is-failed' : ''}><span>03</span><div><strong>{progress.stage === 'failed' ? 'Needs attention' : progress.stage === 'completed' ? 'Redacted text ready' : 'Removing personal identifiers'}</strong><small>{progress.stage === 'failed' ? 'We could not prepare the redacted text.' : progress.stage === 'redacting' ? progress.message : progress.stage === 'completed' ? 'The redacted resume text is ready for the next step.' : 'Waiting for text extraction to finish.'}</small></div></li>
                </ol>
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
