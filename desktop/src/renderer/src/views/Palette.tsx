import { useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../api'
import { useRefine } from '../store'
import { Icon } from './Icon'

type Item = { id: string; group: 'Tickets' | 'Actions'; label: string; detail?: string; key?: string; run: () => void }

// Palette jumps to a ticket or runs an action. Typing a key or a number
// opens that ticket, listed or not.
export function Palette() {
  const refine = useRefine()
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const input = useRef<HTMLInputElement>(null)
  useEffect(() => input.current?.focus(), [])
  const close = () => refine.setPalette(false)

  const items = useMemo(() => {
    const q = query.trim().toLowerCase()
    const out: Item[] = []
    const rows = refine.page?.rows ?? []
    for (const row of rows) {
      if (q && !row.key.toLowerCase().includes(q) && !row.summary.toLowerCase().includes(q)) continue
      out.push({ id: row.key, group: 'Tickets', key: row.key, label: row.summary, detail: row.step, run: () => refine.open(row.key) })
    }
    const project = refine.info?.project ?? ''
    const typed = /^\d+$/.test(q) ? `${project}-${q}` : /^[a-z][a-z0-9_]*-\d+$/i.test(q) ? q.toUpperCase() : ''
    if (typed && !rows.some((r) => r.key === typed.toUpperCase())) {
      out.unshift({
        id: 'open:' + typed,
        group: 'Tickets',
        key: typed.toUpperCase(),
        label: `Open ${typed.toUpperCase()}`,
        run: () =>
          void api.lookup(typed).then(
            (found) => refine.open(found.key),
            (err) => refine.flash({ text: (err as Error).message, error: true })
          )
      })
    }
    const actions: Item[] = [
      { id: 'refresh', group: 'Actions', label: 'Refresh tickets from Jira', run: () => void refine.refresh() },
      { id: 'tickets', group: 'Actions', label: 'Go to Tickets', run: () => refine.setScreen({ kind: 'tickets' }) },
      { id: 'repos', group: 'Actions', label: 'Go to Repos', run: () => refine.setScreen({ kind: 'repos' }) }
    ]
    if (refine.screen.kind === 'ticket') {
      const key = refine.screen.key
      actions.push({
        id: 'reset',
        group: 'Actions',
        label: `Start ${key} over`,
        detail: 'forgets this refinement',
        run: () =>
          void api.reset(key).then(
            () => {
              refine.open(key)
              void refine.refresh()
            },
            (err) => refine.flash({ text: (err as Error).message, error: true })
          )
      })
    }
    out.push(...actions.filter((a) => !q || a.label.toLowerCase().includes(q)))
    return out
  }, [query, refine])

  useEffect(() => setActive(0), [query])

  const choose = (item?: Item) => {
    if (!item) return
    close()
    item.run()
  }

  let group = ''
  return (
    <div className="overlay top" onClick={close}>
      <section role="dialog" aria-label="Command palette" className="palette" onClick={(e) => e.stopPropagation()}>
        <div className="palette-input">
          <Icon name="search" className="muted" />
          <input
            ref={input}
            aria-label="Search tickets and actions"
            placeholder="Search tickets and actions, or type a key like 101"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Escape') close()
              else if (e.key === 'ArrowDown') setActive((a) => Math.min(a + 1, items.length - 1))
              else if (e.key === 'ArrowUp') setActive((a) => Math.max(a - 1, 0))
              else if (e.key === 'Enter') choose(items[active])
              else return
              e.preventDefault()
            }}
          />
          <kbd>Esc</kbd>
        </div>
        <div className="palette-list">
          {items.length === 0 && <div className="muted" style={{ padding: 10 }}>Nothing matches.</div>}
          {items.map((item, i) => {
            const head = item.group !== group
            group = item.group
            return (
              <div key={item.id}>
                {head && <div className="palette-group">{item.group}</div>}
                <button className={`palette-item ${i === active ? 'active' : ''}`} onMouseEnter={() => setActive(i)} onClick={() => choose(item)}>
                  {item.key && <span className="mono muted" style={{ width: 82 }}>{item.key}</span>}
                  <span className="grow ellipsis">{item.label}</span>
                  {item.detail && <span className="muted small">{item.detail}</span>}
                </button>
              </div>
            )
          })}
        </div>
        <div className="palette-foot">
          <span>↑↓ move</span>
          <span>↵ open</span>
          <span>Type a number to jump to a ticket</span>
        </div>
      </section>
    </div>
  )
}
