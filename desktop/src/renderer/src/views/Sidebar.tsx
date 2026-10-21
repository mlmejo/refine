import { useState, type FormEvent } from 'react'
import { useRefine } from '../store'
import { refined, type Issue } from '../types'

export function Sidebar({ onRepos }: { onRepos: () => void }) {
  const refine = useRefine()
  const [q, setQ] = useState('')
  const [openError, setOpenError] = useState<string>()
  const [opening, setOpening] = useState(false)
  const [moveAsk, setMoveAsk] = useState<string>()

  const open = async (e: FormEvent) => {
    e.preventDefault()
    if (!q.trim()) return
    setOpening(true)
    const error = await refine.openByKey(q)
    setOpening(false)
    setOpenError(error)
    if (!error) setQ('')
  }

  return (
    <aside className="sidebar">
      <header className="sidebar-head">
        <div className="title">{refine.info?.title ?? 'refine'}</div>
        {refine.user && <div className="quiet small">{refine.user}</div>}
      </header>

      <form className="open-key" onSubmit={open}>
        <input
          value={q}
          placeholder={`Open by key, e.g. ${refine.info?.project ?? 'GEEK'}-123`}
          onChange={(e) => {
            setQ(e.target.value)
            setOpenError(undefined)
          }}
          disabled={opening}
        />
      </form>
      {openError && <div className="error-text small pad">{openError}</div>}

      <div className="ticket-list">
        {refine.loading && refine.issues.length === 0 && (
          <div className="quiet pad">
            <span className="spinner" /> Loading tickets…
          </div>
        )}
        {refine.listError && <div className="error-text pad">{refine.listError}</div>}
        {!refine.loading && !refine.listError && refine.issues.length === 0 && (
          <div className="quiet pad">No tickets assigned to you in this sprint.</div>
        )}
        {refine.issues.map((issue) => (
          <Row
            key={issue.key}
            issue={issue}
            pinned={refine.pinned.some((p) => p.key === issue.key)}
            moveAsk={moveAsk === issue.key}
            onMoveAsk={(ask) => setMoveAsk(ask ? issue.key : undefined)}
          />
        ))}
      </div>

      <footer className="sidebar-foot">
        <button onClick={() => void refine.refresh()} disabled={refine.loading} title="Refresh the list">
          {refine.loading ? 'Refreshing…' : 'Refresh'}
        </button>
        <button onClick={onRepos} title="The repos Claude reads">
          Repos
        </button>
        {refine.removed.length > 0 && (
          <button onClick={() => void refine.undo()} title="Put the last removed ticket back">
            Undo remove
          </button>
        )}
      </footer>
    </aside>
  )
}

function Row({
  issue,
  pinned,
  moveAsk,
  onMoveAsk
}: {
  issue: Issue
  pinned: boolean
  moveAsk: boolean
  onMoveAsk: (ask: boolean) => void
}) {
  const refine = useRefine()
  const t = refine.ticket(issue.key)
  const active = refine.selected === issue.key
  const busy = t.job && !t.job.done

  return (
    <div className={`row ${active ? 'active' : ''}`} onClick={() => refine.select(issue.key)}>
      <div className="row-top">
        <span className="key">{issue.key}</span>
        <span className="marks">
          <Mark name="AC" filled={issue.acceptance_criteria !== ''} />
          <Mark name="TI" filled={issue.technical_implementation !== ''} />
        </span>
        {busy && <span className="spinner small" title={`${t.job!.kind} running`} />}
        {pinned && <span className="tag">opened</span>}
      </div>
      <div className="row-summary">{issue.summary}</div>
      {moveAsk ? (
        <div className="row-ask" onClick={(e) => e.stopPropagation()}>
          Move to {refine.info?.ready_sprint}?
          <button
            className="primary"
            onClick={() => {
              onMoveAsk(false)
              void refine.move(issue.key)
            }}
          >
            Move
          </button>
          <button onClick={() => onMoveAsk(false)}>Cancel</button>
        </div>
      ) : (
        <div className="row-actions" onClick={(e) => e.stopPropagation()}>
          {refined(issue) && (
            <button className="link" onClick={() => onMoveAsk(true)} title={`Move to ${refine.info?.ready_sprint}`}>
              move
            </button>
          )}
          <button
            className="link"
            onClick={() => void refine.remove(issue.key)}
            title={pinned ? 'Take it off the list' : 'Hide it from the list for good'}
          >
            remove
          </button>
        </div>
      )}
    </div>
  )
}

function Mark({ name, filled }: { name: string; filled: boolean }) {
  return (
    <span className={filled ? 'mark filled' : 'mark'}>
      {name} {filled ? '✓' : '·'}
    </span>
  )
}
