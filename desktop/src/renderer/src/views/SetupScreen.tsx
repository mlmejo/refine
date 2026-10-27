import { useState } from 'react'

const tokenPage = 'https://id.atlassian.com/manage-profile/security/api-tokens'

// SetupScreen takes the Jira account on first launch, when the config lacks
// it. Saving checks the account with Jira, writes it to the config and starts
// refine.
export function SetupScreen() {
  const [site, setSite] = useState('geekee.atlassian.net')
  const [email, setEmail] = useState('')
  const [token, setToken] = useState('')
  const [error, setError] = useState<string>()
  const [saving, setSaving] = useState(false)

  const ready = site.trim() !== '' && email.trim() !== '' && token.trim() !== ''

  const save = async () => {
    if (!ready || saving) return
    setSaving(true)
    setError(undefined)
    const failed = await window.refine.saveAccount({ site, email, token })
    // On success the app moves on to starting refine, so only a failure
    // lands back here.
    if (failed) {
      setError(failed)
      setSaving(false)
    }
  }

  const field = (label: string, value: string, set: (v: string) => void, props: { type?: string; placeholder?: string } = {}) => (
    <label className="stack">
      <span className="small">{label}</span>
      <input
        className="textarea mono"
        value={value}
        disabled={saving}
        onChange={(e) => set(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && void save()}
        {...props}
      />
    </label>
  )

  return (
    <div className="boot">
      <div className="boot-card">
        <h2>Set up refine</h2>
        <p className="muted">refine reads your Jira tickets with your Atlassian account.</p>
        {error && <div className="banner error">{error}</div>}
        <section className="section">
          {field('Jira site', site, setSite, { placeholder: 'your-team.atlassian.net' })}
          {field('Email', email, setEmail, { type: 'email', placeholder: 'you@example.com' })}
          {field('API token', token, setToken, { type: 'password' })}
          <span className="muted small">
            Make a token at{' '}
            <button className="link" onClick={() => void window.refine.openExternal(tokenPage)}>
              id.atlassian.com
            </button>
            .
          </span>
          <div className="row">
            <button className="btn primary" disabled={!ready || saving} onClick={() => void save()}>
              {saving ? 'Checking with Jira…' : 'Save'}
            </button>
          </div>
          <span className="muted small">Saved in ~/.config/refine/config.toml, readable by you alone.</span>
        </section>
      </div>
    </div>
  )
}
