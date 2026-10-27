import { useState } from 'react'
import { api } from '../api'
import type { ClaudeDir } from '../types'

// ClaudeScreen picks the Claude Code config directory refine's runs use,
// which picks the Claude account: ~/.claude unless the developer keeps one
// per account, such as ~/.claude-team. Without onBack it is the setup shown
// when the directory is missing.
export function ClaudeScreen({ claude, onSaved, onBack }: { claude: ClaudeDir; onSaved: (c: ClaudeDir) => void; onBack?: () => void }) {
  const [dir, setDir] = useState(claude.dir)
  const [error, setError] = useState<string>()
  const [saving, setSaving] = useState(false)
  const [done, setDone] = useState<string>()

  const pick = async () => {
    const picked = await window.refine.pickFolder(dir || undefined, 'claude')
    if (picked) setDir(picked)
  }

  const save = async () => {
    setSaving(true)
    setError(undefined)
    setDone(undefined)
    try {
      const saved = await api.setClaudeDir(dir)
      setDir(saved.dir)
      setDone(`Claude runs use ${saved.dir}`)
      onSaved(saved)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  const form = (
    <section className="section">
      {!claude.found && (
        <div className="banner error">
          No Claude Code config at <span className="mono">{claude.dir}</span>. Pick the folder your Claude Code account
          uses: the one <span className="mono">CLAUDE_CONFIG_DIR</span> points at, if you set it.
        </div>
      )}
      {error && <div className="banner error">{error}</div>}
      {done && !error && <div className="done-text small">{done}</div>}
      <div className="row">
        <input
          className="textarea mono grow"
          aria-label="Claude config folder"
          placeholder="~/.claude"
          value={dir}
          disabled={saving}
          onChange={(e) => setDir(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && void save()}
        />
        <button className="btn" disabled={saving} onClick={() => void pick()}>
          Browse…
        </button>
      </div>
      <div className="row">
        <button className="btn primary" disabled={saving || !dir.trim() || (claude.found && dir === claude.dir)} onClick={() => void save()}>
          {saving ? 'Saving…' : 'Use this folder'}
        </button>
      </div>
      <span className="muted small">Saved as claude.config_dir in ~/.config/refine/config.toml.</span>
    </section>
  )

  if (!onBack) {
    return (
      <div className="boot">
        <div className="boot-card">
          <h2>Which Claude Code account?</h2>
          {form}
        </div>
      </div>
    )
  }
  return (
    <>
      <header className="topbar">
        <h1>Claude</h1>
        <span className="muted">The Claude Code config folder, and so the account, refine's runs use</span>
      </header>
      <div className="scroll">
        <div className="page">
          <div className="page-main" style={{ maxWidth: 820 }}>
            {form}
          </div>
        </div>
      </div>
      <footer className="footbar thin">
        <button className="link" onClick={onBack}>
          ← Tickets
        </button>
      </footer>
    </>
  )
}
