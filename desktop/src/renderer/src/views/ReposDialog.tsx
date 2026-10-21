import { useEffect, useState } from 'react'
import { api } from '../api'
import type { Found, Repo } from '../types'

// Scan is a picked folder's git checkouts, each with whether it is picked to
// add and the name it will get.
type Scan = { dir: string; rows: (Found & { picked: boolean })[] }

// ReposDialog lists the repos Claude reads, and adds or removes them in
// config.toml. Adding scans a picked folder for git checkouts. A change
// applies to the next brief or draft.
export function ReposDialog({ onClose }: { onClose: () => void }) {
  const [repos, setRepos] = useState<Repo[]>()
  const [error, setError] = useState<string>()
  const [done, setDone] = useState<string>()
  const [scan, setScan] = useState<Scan>()
  const [adding, setAdding] = useState(false)
  const [removeAsk, setRemoveAsk] = useState<string>()

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
        setError(`no git repos in ${dir}`)
        return
      }
      setScan({ dir, rows: found.map((f) => ({ ...f, picked: !f.listed })) })
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const edit = (i: number, change: Partial<Scan['rows'][number]>) =>
    setScan((s) => s && { ...s, rows: s.rows.map((r, j) => (j === i ? { ...r, ...change } : r)) })

  // add adds the picked repos in order, stopping at the first that fails so
  // the rest stay picked.
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
    if (added.length) setDone(`added ${added.join(', ')}`)
    if (rows.some((r) => r.picked && !r.listed)) setScan({ ...scan, rows })
    else setScan(undefined)
  }

  const remove = async (name: string) => {
    setRemoveAsk(undefined)
    try {
      setRepos(await api.removeRepo(name))
      setDone(`removed ${name}`)
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const picked = scan?.rows.filter((r) => r.picked && !r.listed).length ?? 0
  const relative = (dir: string) => (scan && dir.startsWith(scan.dir + '/') ? dir.slice(scan.dir.length + 1) : dir)

  return (
    <div className="overlay" onClick={onClose}>
      <div className="dialog wide" onClick={(e) => e.stopPropagation()}>
        {scan ? (
          <>
            <h2>Add repos</h2>
            <div className="mono small quiet">{scan.dir}</div>
            <div className="repo-list scan-list">
              {scan.rows.map((row, i) => (
                <label key={row.dir} className={`repo-row ${row.listed ? 'listed' : ''}`}>
                  <input
                    type="checkbox"
                    checked={row.picked || !!row.listed}
                    disabled={!!row.listed || adding}
                    onChange={(e) => edit(i, { picked: e.target.checked })}
                  />
                  {row.listed ? (
                    <span className="key name-cell">{row.listed}</span>
                  ) : (
                    <input
                      className="name-cell"
                      value={row.name}
                      disabled={adding}
                      onChange={(e) => edit(i, { name: e.target.value, picked: true })}
                    />
                  )}
                  <span className="mono quiet small grow">{relative(row.dir)}</span>
                  {row.listed && <span className="done-text small">added</span>}
                </label>
              ))}
            </div>
            {error && <p className="error-text">{error}</p>}
            {done && !error && <p className="done-text small">{done}</p>}
            <div className="actions">
              <button className="primary" disabled={picked === 0 || adding} onClick={() => void add()}>
                {adding ? 'Adding…' : `Add ${picked} repo${picked === 1 ? '' : 's'}`}
              </button>
              <button disabled={adding} onClick={() => setScan(undefined)}>
                Back
              </button>
            </div>
          </>
        ) : (
          <>
            <h2>Repos Claude reads</h2>
            {!repos && !error && <span className="spinner" />}
            {repos && repos.length === 0 && <p className="quiet">No repos yet. Add one so Claude can read the code.</p>}
            <div className="repo-list">
              {repos?.map((repo) => (
                <div key={repo.name} className="repo-row">
                  <span className="key">{repo.name}</span>
                  <span className="mono quiet small grow">{repo.dir}</span>
                  {removeAsk === repo.name ? (
                    <>
                      <span className="small">Remove from the config?</span>
                      <button className="danger" onClick={() => void remove(repo.name)}>
                        Remove
                      </button>
                      <button onClick={() => setRemoveAsk(undefined)}>Cancel</button>
                    </>
                  ) : (
                    <button className="link" onClick={() => setRemoveAsk(repo.name)}>
                      remove
                    </button>
                  )}
                </div>
              ))}
            </div>
            {error && <p className="error-text">{error}</p>}
            {done && !error && <p className="done-text small">{done}</p>}
            <div className="actions">
              <button className="primary" onClick={() => void pick()} title="Pick a folder; refine finds the git repos in it">
                Add repos…
              </button>
              <button onClick={onClose}>Close</button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
