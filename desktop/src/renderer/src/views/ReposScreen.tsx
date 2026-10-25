import { useEffect, useState } from 'react'
import { api } from '../api'
import type { Found, QA, Repo } from '../types'
import { Icon } from './Icon'

// Scan is a picked folder's git checkouts, each with whether it is picked to
// add and the name it will get.
type Scan = { dir: string; rows: (Found & { picked: boolean })[] }

function Profile({ repo, onSaved, onError }: { repo: Repo; onSaved: (repos: Repo[]) => void; onError: (e: string) => void }) {
  const [editing, setEditing] = useState(false)
  const [answers, setAnswers] = useState<QA[]>(repo.profile?.answers ?? [])
  const save = async (next: QA[]) => {
    try {
      onSaved(await api.setProfile(repo.name, next))
      setEditing(false)
    } catch (err) {
      onError((err as Error).message)
    }
  }
  if (!repo.profile && !editing) return <span className="muted small">No deploy info yet; asked the first time a ticket needs it.</span>
  if (!editing) {
    return (
      <div className="stack" style={{ gap: 2 }}>
        {(repo.profile?.answers ?? []).map((qa) => (
          <span key={qa.question} className="small">
            <span className="muted">{qa.question}</span> {qa.answer}
          </span>
        ))}
        <div className="row">
          <button className="link" onClick={() => setEditing(true)}>
            Edit
          </button>
          <button className="link" style={{ color: 'var(--muted)' }} onClick={() => void save([])} title="Forget it, so the next ticket asks again">
            Forget
          </button>
        </div>
      </div>
    )
  }
  return (
    <div className="stack">
      {answers.map((qa, i) => (
        <div key={i} className="stack" style={{ gap: 2 }}>
          <span className="muted small">{qa.question}</span>
          <input
            className="textarea"
            aria-label={qa.question}
            value={qa.answer}
            onChange={(e) => setAnswers(answers.map((x, j) => (j === i ? { ...x, answer: e.target.value } : x)))}
          />
        </div>
      ))}
      <div className="row">
        <button className="btn primary" onClick={() => void save(answers)}>
          Save
        </button>
        <button className="btn" onClick={() => setEditing(false)}>
          Cancel
        </button>
      </div>
    </div>
  )
}

// ReposScreen lists the repos Claude can read, with how each deploys, and
// adds or removes them in config.toml. Adding scans a picked folder.
export function ReposScreen({ onBack }: { onBack: () => void }) {
  const [repos, setRepos] = useState<Repo[]>()
  const [error, setError] = useState<string>()
  const [done, setDone] = useState<string>()
  const [scan, setScan] = useState<Scan>()
  const [adding, setAdding] = useState(false)
  const [removeAsk, setRemoveAsk] = useState<string>()
  const [filter, setFilter] = useState('')

  useEffect(() => {
    api.repos().then(setRepos, (err) => setError((err as Error).message))
  }, [])

  const pick = async () => {
    setError(undefined)
    setDone(undefined)
    const { start } = await api.folderStart().catch(() => ({ start: undefined }))
    const dir = await window.refine.pickFolder(start)
    if (!dir) return
    try {
      const found = await api.scanRepos(dir)
      if (found.length === 0) {
        setError(`No git repos in ${dir}`)
        return
      }
      setScan({ dir, rows: found.map((f) => ({ ...f, picked: !f.listed })) })
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const edit = (i: number, change: Partial<Scan['rows'][number]>) =>
    setScan((s) => s && { ...s, rows: s.rows.map((r, j) => (j === i ? { ...r, ...change } : r)) })

  const add = async () => {
    if (!scan) return
    setAdding(true)
    setError(undefined)
    const added: string[] = []
    let rows = scan.rows
    for (const [i, row] of rows.entries()) {
      if (!row.picked || row.listed) continue
      try {
        setRepos(await api.addRepo(row.name, row.dir))
        added.push(row.name.trim())
        rows = rows.map((r, j) => (j === i ? { ...r, picked: false, listed: row.name.trim() } : r))
      } catch (err) {
        setError(`${row.name}: ${(err as Error).message}`)
        break
      }
    }
    setAdding(false)
    if (added.length) setDone(`Added ${added.join(', ')}`)
    if (rows.some((r) => r.picked && !r.listed)) setScan({ ...scan, rows })
    else setScan(undefined)
  }

  const remove = async (name: string) => {
    setRemoveAsk(undefined)
    try {
      setRepos(await api.removeRepo(name))
      setDone(`Removed ${name}`)
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const picked = scan?.rows.filter((r) => r.picked && !r.listed).length ?? 0
  const shown = (repos ?? []).filter((r) => r.name.toLowerCase().includes(filter.trim().toLowerCase()))

  return (
    <>
      <header className="topbar">
        <h1>Repos</h1>
        <span className="muted">The repos Claude can read, and how each deploys</span>
        <button className="btn" style={{ marginLeft: 'auto' }} onClick={() => void pick()}>
          Add repos…
        </button>
      </header>
      <div className="scroll">
        <div className="page">
          <div className="page-main" style={{ maxWidth: 820 }}>
            {error && <div className="banner error">{error}</div>}
            {done && !error && <div className="done-text small">{done}</div>}
            {scan ? (
              <section className="section">
                <div className="section-head">
                  <h2>Add repos</h2>
                  <span className="mono muted small">{scan.dir}</span>
                </div>
                {scan.rows.map((row, i) => (
                  <label key={row.dir} className="repo-pick">
                    <input
                      type="checkbox"
                      checked={row.picked || !!row.listed}
                      disabled={!!row.listed || adding}
                      onChange={(e) => edit(i, { picked: e.target.checked })}
                    />
                    {row.listed ? (
                      <span className="mono">{row.listed}</span>
                    ) : (
                      <input className="inline-edit mono" style={{ maxWidth: 220 }} value={row.name} disabled={adding} onChange={(e) => edit(i, { name: e.target.value, picked: true })} />
                    )}
                    <span className="mono muted small grow ellipsis">{row.dir.startsWith(scan.dir + '/') ? row.dir.slice(scan.dir.length + 1) : row.dir}</span>
                    {row.listed && <span className="done-text small">added</span>}
                  </label>
                ))}
                <div className="row">
                  <button className="btn primary" disabled={picked === 0 || adding} onClick={() => void add()}>
                    {adding ? 'Adding…' : `Add ${picked} repo${picked === 1 ? '' : 's'}`}
                  </button>
                  <button className="btn" disabled={adding} onClick={() => setScan(undefined)}>
                    Back
                  </button>
                </div>
              </section>
            ) : (
              <>
                <div className="filter">
                  <Icon name="search" className="muted" />
                  <input aria-label="Filter repos" placeholder={`Filter ${repos?.length ?? ''} repos`} value={filter} onChange={(e) => setFilter(e.target.value)} />
                </div>
                {!repos && !error && <span className="spinner" />}
                {repos && repos.length === 0 && <p className="muted">No repos yet. Add some so Claude can read the code.</p>}
                {shown.map((repo) => (
                  <div key={repo.name} className="card">
                    <div className="row">
                      <span className="mono" style={{ fontWeight: 500 }}>
                        {repo.name}
                      </span>
                      <span className="mono muted small grow ellipsis">{repo.dir}</span>
                      {removeAsk === repo.name ? (
                        <>
                          <span className="small">Remove from the config?</span>
                          <button className="btn danger" onClick={() => void remove(repo.name)}>
                            Remove
                          </button>
                          <button className="btn" onClick={() => setRemoveAsk(undefined)}>
                            Cancel
                          </button>
                        </>
                      ) : (
                        <button className="link" style={{ color: 'var(--muted)' }} onClick={() => setRemoveAsk(repo.name)}>
                          Remove
                        </button>
                      )}
                    </div>
                    <Profile repo={repo} onSaved={setRepos} onError={setError} />
                  </div>
                ))}
              </>
            )}
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
