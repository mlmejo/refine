import { useEffect, useRef, useState } from 'react'
import { api } from '../../api'
import { useRefine } from '../../store'
import type { Repo, View } from '../../types'
import { Icon } from '../Icon'

// Repos picks the repos Claude reads for this ticket, then fetches each from
// origin. Repos used by related tickets, and those Claude matches to the
// ticket's title and labels, are suggested at the top. A repo that cannot be fetched blocks the next step. Looked back at
// from a later step, the repos can be picked again, which drops what Claude
// read from them.
export function Repos({ view, readOnly }: { view: View; readOnly: boolean }) {
  const refine = useRefine()
  const r = view.refinement
  const key = r.key
  const picked = r.repos ?? []
  const suggested = view.suggested_repos ?? []
  const synced = r.synced ?? []
  const [all, setAll] = useState<Repo[]>()
  const [filter, setFilter] = useState('')
  const [confirming, setConfirming] = useState(false)
  const [suggestError, setSuggestError] = useState<string>()
  const asked = useRef<string>(undefined)
  const { setError, keep } = refine

  useEffect(() => {
    api.repos().then(setAll, (err) => setError(key, (err as Error).message))
  }, [key, setError])

  // Claude suggests repos once, on reaching this step. A failed suggestion
  // only leaves the list without it.
  const suggesting = !readOnly && r.step === 'repos' && !r.suggestion && !suggestError
  useEffect(() => {
    if (!suggesting || asked.current === key) return
    asked.current = key
    api.suggestRepos(key).then(keep, (err) => setSuggestError((err as Error).message))
  }, [suggesting, key, keep])

  const toggle = (name: string) => {
    const next = picked.includes(name) ? picked.filter((n) => n !== name) : [...picked, name]
    void refine.act(key, 'repos', () => api.setRepos(key, next))
  }

  const canChoose = readOnly && ['questions', 'deploy', 'results'].includes(r.step) && !r.stamp.done?.length
  const chooseAgain = () => void refine.act(key, 'choose', () => api.chooseRepos(key))

  const fetchAndAsk = async () => {
    const after = await refine.act(key, 'sync', () => api.sync(key))
    if (after?.refinement.step === 'questions') void refine.run('ask', key)
  }

  useEffect(() => {
    if (readOnly) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Enter' && e.metaKey && picked.length > 0 && !refine.pending) {
        e.preventDefault()
        void fetchAndAsk()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  const matches = (name: string) => name.toLowerCase().includes(filter.trim().toLowerCase())
  const top = suggested.filter((s) => matches(s.repo))
  const rest = (all ?? []).map((a) => a.name).filter((n) => !suggested.some((s) => s.repo === n) && matches(n))
  const branchOf = (name: string) => synced.find((s) => s.name === name)

  const pick = (name: string, note?: string) => (
    <label key={name} className={`repo-pick ${picked.includes(name) ? 'on' : ''}`}>
      <input type="checkbox" checked={picked.includes(name)} disabled={readOnly || !!refine.pending} onChange={() => toggle(name)} />
      <span className="mono">{name}</span>
      {branchOf(name)?.branch && <span className="chip">{branchOf(name)?.branch}</span>}
      <span className="muted small" style={{ marginLeft: 'auto' }}>
        {note}
      </span>
    </label>
  )

  return (
    <>
      <div className="scroll">
        <div className="page">
          <section className="page-main">
            <div className="section-head">
              <h2>Repos Claude reads for this ticket</h2>
              <span className="muted small">Remembered for {key}</span>
            </div>
            {readOnly ? (
              <div className="stack">{picked.map((n) => pick(n))}</div>
            ) : (
              <>
                <div className="filter">
                  <Icon name="search" className="muted" />
                  <input aria-label="Filter repos" placeholder={`Filter ${all?.length ?? ''} repos`} value={filter} onChange={(e) => setFilter(e.target.value)} />
                </div>
                {(top.length > 0 || suggesting || suggestError) && (
                  <div className="stack" style={{ gap: 1 }}>
                    <span className="nav-label" style={{ padding: '6px 10px 0' }}>
                      Suggested
                    </span>
                    {top.map((s) => pick(s.repo, s.reason))}
                    {suggesting && (
                      <span className="row muted small" style={{ padding: '6px 10px' }}>
                        <span className="spinner" /> Claude is matching repos to the title and labels…
                      </span>
                    )}
                    {suggestError && (
                      <span className="muted small" style={{ padding: '6px 10px' }}>
                        Claude could not suggest repos: {suggestError}
                      </span>
                    )}
                  </div>
                )}
                <div className="stack" style={{ gap: 1 }}>
                  <span className="nav-label" style={{ padding: '6px 10px 0' }}>
                    All repos
                  </span>
                  {!all && <span className="spinner" />}
                  {all && all.length === 0 && <span className="muted">No repos yet. Add them under Repos in the sidebar.</span>}
                  {rest.map((n) => pick(n))}
                </div>
              </>
            )}
          </section>
          <aside className="page-side">
            <div className="card">
              <h3>Sync</h3>
              <p className="muted small" style={{ margin: 0 }}>
                Claude reads each repo at develop, or at its default branch when it has no develop, as on origin. It reads a separate worktree, so
                your checkout and local changes are not touched.
              </p>
              {synced.length > 0 && (
                <div className="stack" style={{ gap: 0, borderTop: '1px solid var(--border)' }}>
                  {synced.map((s) => (
                    <div key={s.name} className="sync-row">
                      <span className="mono grow">{s.name}</span>
                      {s.error ? (
                        <span className="small error-text" style={{ flexBasis: '100%' }}>
                          {s.error}
                        </span>
                      ) : (
                        <>
                          <span className="mono small muted">origin/{s.branch}</span>
                          <span className="small" style={{ color: s.new ? 'var(--amber)' : 'var(--green)' }}>
                            {s.new ? `${s.new} new commit${s.new === 1 ? '' : 's'}` : 'Up to date'}
                          </span>
                        </>
                      )}
                    </div>
                  ))}
                </div>
              )}
              {synced.some((s) => s.error) && <span className="small error-text">Fix the repos above or unpick them, then fetch again.</span>}
            </div>
          </aside>
        </div>
      </div>
      {canChoose && (
        <footer className="footbar">
          {confirming ? (
            <>
              <span className="small">Picking again drops the question rounds, the deployment and the result.</span>
              <button className="btn" style={{ marginLeft: 'auto' }} disabled={!!refine.pending} onClick={() => setConfirming(false)}>
                Keep them
              </button>
              <button className="btn primary" disabled={!!refine.pending} onClick={chooseAgain}>
                {refine.pending === 'choose' ? 'Dropping…' : 'Drop and pick again'}
              </button>
            </>
          ) : (
            <button className="btn" style={{ marginLeft: 'auto' }} disabled={!!refine.pending} onClick={() => setConfirming(true)}>
              Pick repos again
            </button>
          )}
        </footer>
      )}
      {!readOnly && (
        <footer className="footbar">
          <span className="muted small">
            {picked.length} repo{picked.length === 1 ? '' : 's'} selected
          </span>
          <button className="btn primary" style={{ marginLeft: 'auto' }} disabled={picked.length === 0 || !!refine.pending} onClick={() => void fetchAndAsk()}>
            {refine.pending === 'sync' ? 'Fetching…' : 'Fetch and start questions'} <kbd>⌘↵</kbd>
          </button>
        </footer>
      )}
    </>
  )
}
