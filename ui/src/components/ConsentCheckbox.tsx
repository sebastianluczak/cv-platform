import React from 'react'

interface ConsentCheckboxProps {
  checked: boolean
  onChange: (checked: boolean) => void
}

export default function ConsentCheckbox({ checked, onChange }: ConsentCheckboxProps) {
  const handleChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    onChange(event.target.checked)
  }

  return (
    <div className="consent-container">
      <label className="consent-checkbox">
        <input
          type="checkbox"
          checked={checked}
          onChange={handleChange}
          id="consent-policies"
          aria-label="Consent to privacy and data retention policies"
        />
        <span className="consent-checkbox-label">
          I understand and agree to the{' '}
          <a href="/privacy" className="consent-link" target="_blank" rel="noopener noreferrer">
            Privacy Policy
          </a>
          {' '}and{' '}
          <a href="/data-retention" className="consent-link" target="_blank" rel="noopener noreferrer">
            Data Retention Policy
          </a>
        </span>
      </label>
    </div>
  )
}
