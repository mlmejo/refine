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

// Tickets lists the unrefined tickets: started ones pinned on top.
export function Tickets() {
  const refine = useRefine()
  const rows = useMemo(() => refine.page?.rows ?? [], [refine.page])
  const started = rows.filter((r) => r.started)
  const rest = rows.filter((r) => !r.started)
  const ordered = [...started, ...rest]
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
      <>
        <div className="list-head">
          {title} <span className="chip">{items.length}</span>
        </div>
        {items.map((row) => {
          const i = index++
          return (
            <TicketRow key={row.key} row={row} cursor={i === cursor} onOpen={() => refine.open(row.key)} onHide={() => void hide(row.key)} />
          )
        })}
      </>
    )

  return (
    <>
      <header className="topbar">
        <h1>Tickets</h1>
        <span className="muted">Unrefined, assigned to you</span>
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
          {list('Not started', rest)}
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
