import Footer from './Footer'

export default function DataRetentionPolicy() {
  return (
    <>
      <header className="topbar">
        <a className="brand" href="#/" aria-label="Draftline home">
          <span className="brand-mark">d.</span>
          <span>draftline</span>
        </a>
        <span className="topbar-note">Technical experience, in focus</span>
      </header>

      <div className="privacy-policy-page">
        <article className="privacy-policy-content">
          <header className="privacy-header">
            <p className="eyebrow">
              <span></span>
              Data Handling
            </p>
            <h1>Data Retention Policy</h1>
            <p className="privacy-intro">
              Retention depends on the data type and the platform operator's storage configuration. This page summarizes the current defaults and identifies data that requires operator-managed lifecycle rules.
            </p>
          </header>

          <section className="privacy-section">
            <h2>Overview</h2>
            <p>
              The CV Platform processes resume files through upload, text extraction, redaction, and skill matching. Some data is expired by the datacleaner; other results, including optional AI role insights, remain in object storage unless the operator configures lifecycle deletion. Ask the operator which retention policy applies to this deployment.
            </p>
          </section>

          <section className="privacy-section">
            <h2>Retention Timeline</h2>
            <p>
              The following table outlines what data is stored, where it is stored, and for how long:
            </p>
            <div style={{ overflowX: 'auto' }}>
              <table style={{
                width: '100%',
                borderCollapse: 'collapse',
                fontSize: '12px',
                lineHeight: '1.5',
              }}>
                <thead>
                  <tr style={{ borderBottom: '2px solid #176b5b', backgroundColor: '#e8f3ef' }}>
                    <th style={{
                      padding: '12px',
                      textAlign: 'left',
                      fontWeight: 650,
                      color: '#176b5b',
                    }}>Data Type</th>
                    <th style={{
                      padding: '12px',
                      textAlign: 'left',
                      fontWeight: 650,
                      color: '#176b5b',
                    }}>Location</th>
                    <th style={{
                      padding: '12px',
                      textAlign: 'left',
                      fontWeight: 650,
                      color: '#176b5b',
                    }}>Retention Period</th>
                    <th style={{
                      padding: '12px',
                      textAlign: 'left',
                      fontWeight: 650,
                      color: '#176b5b',
                    }}>Details</th>
                  </tr>
                </thead>
                <tbody>
                  <tr style={{ borderBottom: '1px solid #dce4e6', backgroundColor: '#fff' }}>
                    <td style={{ padding: '12px', color: '#18232b', fontWeight: 600 }}>
                      Original resume file
                    </td>
                    <td style={{ padding: '12px', color: '#52616a' }}>
                      S3 storage (<code style={{ backgroundColor: '#f4f7f6', padding: '2px 4px', borderRadius: '3px' }}>uploads/&lt;job-id&gt;</code>)
                    </td>
                    <td style={{ padding: '12px', color: '#18232b', fontWeight: 600 }}>
                      7 days
                    </td>
                    <td style={{ padding: '12px', color: '#52616a' }}>
                      Deleted automatically; extracted text is used for analysis
                    </td>
                  </tr>
                  <tr style={{ borderBottom: '1px solid #dce4e6', backgroundColor: '#fbfcfc' }}>
                    <td style={{ padding: '12px', color: '#18232b', fontWeight: 600 }}>
                      Extracted text (unredacted)
                    </td>
                    <td style={{ padding: '12px', color: '#52616a' }}>
                      S3 storage (<code style={{ backgroundColor: '#f4f7f6', padding: '2px 4px', borderRadius: '3px' }}>results/&lt;job-id&gt;.txt</code>)
                    </td>
                    <td style={{ padding: '12px', color: '#18232b', fontWeight: 600 }}>
                      Immediately after redaction
                    </td>
                    <td style={{ padding: '12px', color: '#52616a' }}>
                      Deleted right after redaction to minimize PII storage
                    </td>
                  </tr>
                  <tr style={{ borderBottom: '1px solid #dce4e6', backgroundColor: '#fff' }}>
                    <td style={{ padding: '12px', color: '#18232b', fontWeight: 600 }}>
                      Redacted text
                    </td>
                    <td style={{ padding: '12px', color: '#52616a' }}>
                      S3 storage (<code style={{ backgroundColor: '#f4f7f6', padding: '2px 4px', borderRadius: '3px' }}>results/&lt;job-id&gt;.redacted.txt</code>)
                    </td>
                    <td style={{ padding: '12px', color: '#18232b', fontWeight: 600 }}>
                      7 days
                    </td>
                    <td style={{ padding: '12px', color: '#52616a' }}>
                      Best-effort redaction; contains [PLACEHOLDER] for detected PII
                    </td>
                  </tr>
                  <tr style={{ borderBottom: '1px solid #dce4e6', backgroundColor: '#fbfcfc' }}>
                    <td style={{ padding: '12px', color: '#18232b', fontWeight: 600 }}>
                      Skill match results
                    </td>
                    <td style={{ padding: '12px', color: '#52616a' }}>
                      S3 storage (<code style={{ backgroundColor: '#f4f7f6', padding: '2px 4px', borderRadius: '3px' }}>results/&lt;job-id&gt;.skills.json</code>)
                    </td>
                    <td style={{ padding: '12px', color: '#18232b', fontWeight: 600 }}>
                      7 days
                    </td>
                    <td style={{ padding: '12px', color: '#52616a' }}>
                      Category scores and pattern matches; deleted with redacted text
                    </td>
                  </tr>
                  <tr style={{ borderBottom: '1px solid #dce4e6', backgroundColor: '#fff' }}>
                    <td style={{ padding: '12px', color: '#18232b', fontWeight: 600 }}>
                      Optional AI role insights
                    </td>
                    <td style={{ padding: '12px', color: '#52616a' }}>
                      S3 storage (<code style={{ backgroundColor: '#f4f7f6', padding: '2px 4px', borderRadius: '3px' }}>results/&lt;insight-id&gt;.insights.json</code>)
                    </td>
                    <td style={{ padding: '12px', color: '#18232b', fontWeight: 600 }}>
                      Operator-managed
                    </td>
                    <td style={{ padding: '12px', color: '#52616a' }}>
                      The current datacleaner does not delete these artifacts; an S3 lifecycle rule or other deletion process is required
                    </td>
                  </tr>
                  <tr style={{ borderBottom: '1px solid #dce4e6', backgroundColor: '#fff' }}>
                    <td style={{ padding: '12px', color: '#18232b', fontWeight: 600 }}>
                      Failed processing jobs
                    </td>
                    <td style={{ padding: '12px', color: '#52616a' }}>
                      RabbitMQ queue
                    </td>
                    <td style={{ padding: '12px', color: '#18232b', fontWeight: 600 }}>
                      7 days
                    </td>
                    <td style={{ padding: '12px', color: '#52616a' }}>
                      Retained for operator inspection; auto-purged after 7 days
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>

          <section className="privacy-section">
            <h2>What Happens After Retention Expires</h2>
            <ul className="privacy-list">
              <li>
                <strong>Configured expirations are applied.</strong> The datacleaner deletes only data covered by its implemented policies; operators must configure object-storage lifecycle rules for other artifacts, including AI role insights.
              </li>
              <li>
                <strong>No recovery is possible.</strong> Once deleted, data cannot be recovered from the platform. Plan accordingly if you need to retain results beyond the retention window.
              </li>
              <li>
                <strong>No backups are retained by the platform.</strong> The CV Platform does not maintain backups of processed resume data. If you need to keep results for compliance or reference, download or export them during the retention period.
              </li>
            </ul>
          </section>

          <section className="privacy-section">
            <h2>Manual Deletion</h2>
            <p>
              <strong>Contact the platform operator</strong> to request earlier deletion of specific job data. The operator can manually delete job data at any time, bypassing the normal retention window. This is useful when a candidate withdraws consent, requests privacy rights, or when data is no longer needed.
            </p>
          </section>

          <section className="privacy-section" style={{
            borderColor: '#e9c9c4',
            backgroundColor: '#fffafa',
            borderLeft: '3px solid #d47857',
            borderRadius: '2px',
            padding: '12px',
            marginLeft: '-12px',
            marginRight: '-12px',
            paddingLeft: '12px',
          }}>
            <h2 style={{ color: '#a13f35' }}>Important Disclaimer</h2>
            <ul className="privacy-list">
              <li>
                <strong>Redaction is best-effort and may miss some PII.</strong> The redaction process uses pattern matching to identify and replace personally identifiable information. However, it is not guaranteed to catch all PII, especially in unusual formats or contexts.
              </li>
              <li>
                <strong>Original extracted text is separate from the redacted version</strong> and must be treated as sensitive data. This unredacted text is stored temporarily and deleted immediately after redaction, but data in transit or in memory may contain the full extracted content.
              </li>
              <li>
                <strong>Retain this policy and review with legal/compliance before deployment.</strong> This document describes the current system behavior. Before using the CV Platform in a production environment that processes real candidate data, review this policy with your legal and compliance teams to ensure it meets your organizational requirements and applicable data protection regulations.
              </li>
            </ul>
          </section>

          <footer className="privacy-footer">
            <p>
              <small>Last updated: October 2026</small>
            </p>
          </footer>
        </article>
      </div>

      <Footer />
    </>
  )
}
