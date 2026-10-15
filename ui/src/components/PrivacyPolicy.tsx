import Footer from './Footer'

export default function PrivacyPolicy() {
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
            <h1>Privacy Policy</h1>
            <p className="privacy-intro">
              This Privacy Policy explains how the CV Platform collects, uses, and protects your data. Our platform is designed to process your resume data locally and securely.
            </p>
          </header>

          <section className="privacy-section">
            <h2>Overview</h2>
            <p>
              The CV Platform is a resume-processing service that analyzes your CV to extract information, identify personal data for redaction, and match your skills against job requirements. We are committed to protecting your privacy and being transparent about how we handle your data.
            </p>
          </section>

          <section className="privacy-section">
            <h2>Data We Collect</h2>
            <p>When you use the CV Platform, we collect the following data:</p>
            <ul className="privacy-list">
              <li>
                <strong>Uploaded resume files:</strong> PDF documents or images that you upload to be analyzed. These files are stored in our S3-compatible object storage.
              </li>
              <li>
                <strong>Extracted text:</strong> The text extracted from your resume during processing, stored separately for technical analysis and pattern matching.
              </li>
              <li>
                <strong>Redacted text:</strong> A processed version of the extracted text with best-effort personal information removal, generated during the privacy-filtering stage.
              </li>
              <li>
                <strong>Skill matching results:</strong> Category scores and detected skill patterns from your resume, stored as structured data including confidence scores and matched evidence.
              </li>
              <li>
                <strong>Processing metadata:</strong> Job identifiers, processing timestamps, and stage information to track your submission through our pipeline.
              </li>
            </ul>
          </section>

          <section className="privacy-section">
            <h2>How We Use Your Data</h2>
            <p>We use the data we collect exclusively for the following purposes:</p>
            <ul className="privacy-list">
              <li>
                <strong>Text extraction:</strong> Converting your resume from PDF or image format into structured text for analysis.
              </li>
              <li>
                <strong>Named-entity redaction:</strong> Identifying and redacting personal information such as names, phone numbers, and email addresses using local pattern matching and English language models. This is a best-effort process and may not catch all personal identifiers.
              </li>
              <li>
                <strong>Skill matching:</strong> Analyzing your extracted resume text to identify skills and match them against job descriptions you provide, generating compatibility scores.
              </li>
              <li>
                <strong>Service improvement:</strong> Monitoring system performance and error rates to maintain reliability (no individual analysis of resume content).
              </li>
            </ul>
            <p>
              We do not use your data for marketing, advertising, or any purpose outside these stated uses. Your resume content is never shared with third parties for analysis or training purposes.
            </p>
          </section>

          <section className="privacy-section">
            <h2>Data Security</h2>
            <p>We implement the following security measures:</p>
            <ul className="privacy-list">
              <li>
                <strong>Local processing:</strong> All text extraction, named-entity redaction, and skill matching are performed locally on our servers. Your resume data is never sent to external APIs or cloud services.
              </li>
              <li>
                <strong>S3-compatible storage:</strong> Your uploaded files and processing results are stored in S3-compatible object storage. Access is controlled using operator-managed credentials configured in the deployment environment.
              </li>
              <li>
                <strong>Transport security:</strong> Data transmitted between your browser and our platform uses HTTPS encryption.
              </li>
              <li>
                <strong>Operator responsibility:</strong> The security of stored data depends on proper configuration by the platform operator, including secure credential management, network access restrictions, and regular security audits.
              </li>
            </ul>
            <p className="privacy-note">
              <strong>Important:</strong> Redaction is a best-effort process using pattern matching and language models. It is not guaranteed to identify and remove all personal information. The original extracted text remains stored and must continue to be protected as personal data.
            </p>
          </section>

          <section className="privacy-section">
            <h2>Data Retention</h2>
            <p>
              Your data is retained according to the retention policy configured by the platform operator. Typically:
            </p>
            <ul className="privacy-list">
              <li>Uploaded files are retained to support dispute resolution and audit trails.</li>
              <li>Extracted and redacted text are retained for the duration of your use and the configured retention period.</li>
              <li>Processing results are retained for the configured retention period, after which they are typically deleted.</li>
            </ul>
            <p>
              Please contact your platform operator for the specific data retention period that applies to your use of this service. You can request deletion of your data at any time by contacting the operator.
            </p>
          </section>

          <section className="privacy-section">
            <h2>Your Rights</h2>
            <p>
              Depending on your location and applicable privacy laws (such as GDPR or CCPA), you may have the following rights:
            </p>
            <ul className="privacy-list">
              <li><strong>Right to access:</strong> You can request a copy of the data we hold about you.</li>
              <li><strong>Right to deletion:</strong> You can request deletion of your data after the retention period or at any time by contacting the operator.</li>
              <li><strong>Right to rectification:</strong> You can request correction of inaccurate data.</li>
              <li><strong>Right to data portability:</strong> You can request your data in a portable format.</li>
            </ul>
            <p>
              To exercise any of these rights, please contact the platform operator using the contact information provided in the next section.
            </p>
          </section>

          <section className="privacy-section">
            <h2>Contact</h2>
            <p>
              For privacy inquiries, data access requests, deletion requests, or concerns about how your data is handled, please contact:
            </p>
            <div className="privacy-contact">
              <p>
                <strong>Platform Operator Privacy Contact</strong>
                <br />
                Email: <a href="mailto:[operator-email]">[operator-email]</a>
              </p>
            </div>
            <p>
              Please include "Privacy Request" in the subject line of your email, along with a description of your request and any relevant details (such as your job submission ID if applicable).
            </p>
          </section>

          <section className="privacy-section">
            <h2>Changes to This Policy</h2>
            <p>
              We may update this Privacy Policy from time to time to reflect changes in our practices or legal requirements. We will notify you of material changes by updating the policy on this page and noting the revision date. Your continued use of the platform after changes indicates acceptance of the updated policy.
            </p>
          </section>

          <footer className="privacy-footer">
            <p>
              <small>
                Last updated: October 2026
              </small>
            </p>
          </footer>
        </article>
      </div>

      <Footer />
    </>
  )
}
