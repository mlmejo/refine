import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { useRefine } from '../store'
import type { Row } from '../types'
import { typing } from '../typing'

// age says how long ago a time was, in the largest whole unit.
export function age(iso?: string): string {
  if (!iso) return ''
  const minutes = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 60000))
  if (minutes < 1) return 'now'
  if (minutes < 60) return `${minutes}m`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `${hours}h`
  const days = Math.round(hours / 24)
  return days < 7 ? `${days}d` : `${Math.round(days / 7)}w`
}

function TicketRow({ row, cursor, onOpen, onHide }: { row: Row; cursor: boolean; onOpen: () => void; onHide: () => void }) {
  const blockedBy = row.blocked_by ?? []
  const blocks = row.blocks ?? []
  return (
    <div
      role="button"
      tabIndex={-1}
      className={`ticket-row ${row.started ? 'started' : ''} ${cursor ? 'cursor' : ''}`}
      onClick={onOpen}
    >
      <span className="key">{row.key}</span>
      <span className="title">{row.summary}</span>
      {blockedBy.length > 0 && (
        <span className="chip red">
          blocked by <span className="mono">{blockedBy.join(', ')}</span>
        </span>
      )}
      {blocks.length > 0 && (
        <span className="chip" title={blocks.join(', ')}>
          blocks {blocks.length}
        </span>
      )}
      <button
        className="link hide"
        onClick={(e) => {
          e.stopPropagation()
          onHide()
        }}
      >
        Hide
      </button>
      <span className="step">{row.step}</span>
      <span className="age">{age(row.updated)}</span>
    </div>
  )
}

// ReadyRow is a ticket already in the ready sprint, with its points.
function ReadyRow({ row, cursor, onOpen }: { row: Row; cursor: boolean; onOpen: () => void }) {
  return (
    <div role="button" tabIndex={-1} className={`ticket-row ${cursor ? 'cursor' : ''}`} onClick={onOpen}>
      <span className="key">{row.key}</span>
      <span className="title">{row.summary}</span>
      <span className="points">{row.points ?? '–'}</span>
    </div>
  )
}

// sprintMinimum is the fewest points a sprint starts with.
const sprintMinimum = 12

// Tickets lists the unrefined tickets: started ones pinned on top, then the
// ready sprint, then the rest.
export function Tickets() {
  const refine = useRefine()
  const rows = useMemo(() => refine.page?.rows ?? [], [refine.page])
  const ready = useMemo(() => refine.page?.ready ?? [], [refine.page])
  const started = rows.filter((r) => r.started)
  const rest = rows.filter((r) => !r.started)
  const ordered = [...started, ...ready, ...rest]
  const points = ready.reduce((sum, r) => sum + (r.points ?? 0), 0)
  const [cursor, setCursor] = useState(0)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (refine.palette || typing(e)) return
      if (e.key === 'j' || e.key === 'ArrowDown') setCursor((c) => Math.min(c + 1, ordered.length - 1))
      else if (e.key === 'k' || e.key === 'ArrowUp') setCursor((c) => Math.max(c - 1, 0))
      else if (e.key === 'Enter' && ordered[cursor]) refine.open(ordered[cursor].key)
      else return
      e.preventDefault()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [ordered, cursor, refine])

  const hide = async (key: string) => {
    await api.hide(key).catch(() => undefined)
    refine.flash({ text: `Hid ${key}. Open it with ⌘K to show it again.` })
    void refine.refresh()
  }

  let index = 0
  const list = (title: string, items: Row[]) =>
    items.length > 0 && (
      <div className="list-group">
        <div className="list-head">
          {title} <span className="chip">{items.length}</span>
        </div>
        {items.map((row) => {
          const i = index++
          return (
            <TicketRow key={row.key} row={row} cursor={i === cursor} onOpen={() => refine.open(row.key)} onHide={() => void hide(row.key)} />
          )
        })}
      </div>
    )

  return (
    <>
      <header className="topbar">
        <h1>{refine.page?.user ? `Tickets for ${refine.page.user}` : 'Tickets'}</h1>
        <button className="btn" style={{ marginLeft: 'auto' }} disabled={refine.loading} onClick={() => void refine.refresh()}>
          {refine.loading ? 'Refreshing…' : 'Refresh'}
        </button>
      </header>
      <div className="scroll">
        {refine.pageError && <div className="empty error-text">{refine.pageError}</div>}
        {!refine.page && !refine.pageError && (
          <div className="empty row" style={{ justifyContent: 'center' }}>
            <span className="spinner" /> Reading your tickets from Jira…
          </div>
        )}
        {refine.page && rows.length === 0 && (
          <div className="empty">Every ticket in {refine.info?.sprint} is refined.</div>
        )}
        <div style={{ padding: '6px 0' }}>
          {list('Started', started)}
          {refine.page && (
            <div className="list-group">
              <div className="list-head">
                Sprint Ready <span className="chip">{ready.length}</span>
                <span className={`chip ${points >= sprintMinimum ? 'green' : 'amber'}`} title={`${sprintMinimum} points minimum for a sprint`}>
                  {points} / {sprintMinimum} pts
                </span>
              </div>
              {ready.map((row) => {
                const i = index++
                return <ReadyRow key={row.key} row={row} cursor={i === cursor} onOpen={() => refine.open(row.key)} />
              })}
            </div>
          )}
          {list('Unrefined tickets', rest)}
        </div>
      </div>
      <footer className="footbar thin">
        <span>
          <kbd>J</kbd> <kbd>K</kbd> move
        </span>
        <span>
          <kbd>↵</kbd> open
        </span>
        <span>
          <kbd>⌘K</kbd> search and actions
        </span>
      </footer>
    </>
  )
}
