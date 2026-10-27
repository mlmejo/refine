import { useEffect, useRef, useState } from 'react'
import { api } from '../../api'
import { useRefine } from '../../store'
import type { Format, FormatField, Rating, Ratings, RepoFiles, Result, View } from '../../types'
import { StampDialog } from './StampDialog'

const rows: { key: keyof Ratings; label: string }[] = [
  { key: 'novelty', label: 'Novelty & Complexity of Implementation' },
  { key: 'risk', label: 'Risk & Feature Impact' },
  { key: 'dependency', label: 'Dependency & Integration Complexity' },
  { key: 'testing', label: 'Testing & QA Effort' },
  { key: 'deployment', label: 'Deployment Complexity' }
]

const value: Record<string, number> = { Low: 1, Medium: 2, High: 3, 'N/A': 0 }

// score works out the points as refine does: the average of the five
// ratings, rounded.
export function score(r: Ratings): { points: number; average: number } | undefined {
  const all = rows.map(({ key }) => r[key])
  if (all.some((v, i) => !(v in value) || (v === 'N/A' && rows[i].key !== 'testing'))) return undefined
  const average = all.reduce((sum, v) => sum + value[v], 0) / 5
  return { points: Math.round(average), average }
}

// filesText writes a repo's files one per line, new ones marked.
const filesText = (r: RepoFiles) => (r.files ?? []).map((f) => f.path + (f.new ? ' (new)' : '')).join('\n')
const parseFiles = (text: string) =>
  text
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)
    .map((l) => ({ path: l.replace(/\s*\(new\)$/, ''), new: /\(new\)$/.test(l) }))

function Lines({ lines, readOnly, onChange, placeholder }: { lines: string[]; readOnly: boolean; onChange: (lines: string[]) => void; placeholder: string }) {
  const set = (i: number, text: string) => onChange(lines.map((l, j) => (j === i ? text : l)))
  return (
    <ul className="ac-list">
      {lines.map((line, i) => (
        <li key={i}>
          <span className="bullet" aria-hidden="true">
            •
          </span>
          <textarea
            className="inline-edit"
            rows={1}
            value={line}
            readOnly={readOnly}
            aria-label={`${placeholder} ${i + 1}`}
            onChange={(e) => set(i, e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey && !e.metaKey) {
                e.preventDefault()
                onChange([...lines.slice(0, i + 1), '', ...lines.slice(i + 1)])
              } else if (e.key === 'Backspace' && line === '' && lines.length > 1) {
                e.preventDefault()
                onChange(lines.filter((_, j) => j !== i))
              }
            }}
          />
        </li>
      ))}
      {!readOnly && (
        <li>
          <button className="link" style={{ color: 'var(--muted)', padding: '4px 22px' }} onClick={() => onChange([...lines, ''])}>
            Add a line
          </button>
        </li>
      )}
    </ul>
  )
}

// FormatPicker says which rules a field is written with, and takes an
// uploaded .md file of the developer's own.
function FormatPicker({ field }: { field: FormatField }) {
  const refine = useRefine()
  const [format, setFormat] = useState<Format>()
  const upload = useRef<HTMLInputElement>(null)

  useEffect(() => {
    void api.format(field).then(setFormat, () => undefined)
  }, [field])

  const change = async (next: () => Promise<Format>, done: string) => {
    try {
      setFormat(await next())
      refine.flash({ text: done })
    } catch (err) {
      refine.flash({ text: (err as Error).message, error: true })
    }
  }

  return (
    <div className="row small" style={{ gap: 8, marginBottom: 8 }}>
      <span className="muted" title={format?.path}>
        Written with {format?.custom ? 'your format file' : 'the default format'}.
      </span>
      <button className="link" onClick={() => upload.current?.click()}>
        Upload a format (.md)
      </button>
      {format?.custom && (
        <button className="link" onClick={() => void change(() => api.resetFormat(field), 'The default format is back')}>
          Use the default
        </button>
      )}
      <input
        ref={upload}
        type="file"
        accept=".md,.markdown,text/markdown,text/plain"
        hidden
        onChange={(e) => {
          const file = e.target.files?.[0]
          e.target.value = ''
          if (file) void change(async () => api.uploadFormat(field, await file.text()), `Uploaded ${file.name}; regenerate to use it`)
        }}
      />
    </div>
  )
}

// A five-point star of radius 1, centred on the origin.
const STAR = 'M0-1L.235-.324L.951-.309L.38.124L.588.809L0 .4L-.588.809L-.38.124L-.951-.309L-.235-.324Z'

// Seal is the round stamp a stamped ticket's results carry as a watermark:
// rings with stars, crossed by a tilted banner, in worn ink.
function Seal({ slam }: { slam: boolean }) {
  const stars = (y: number) =>
    [
      [-16, 5],
      [0, 7],
      [16, 5],
    ].map(([dx, r]) => <path key={dx} d={STAR} transform={`translate(${100 + dx} ${y + (dx ? 2 : 0)}) scale(${r})`} />)
  return (
    <div className={`seal${slam ? ' slam' : ''}`} aria-hidden="true">
      <svg viewBox="-16 -16 232 232">
        <defs>
          {/* Noise eats holes in the ink and roughens its edges. */}
          <filter id="seal-worn">
            <feTurbulence type="fractalNoise" baseFrequency="0.8" numOctaves="2" seed="7" result="noise" />
            <feColorMatrix in="noise" type="matrix" values="0 0 0 0 0  0 0 0 0 0  0 0 0 0 0  -5 0 0 0 3.4" result="speckle" />
            <feDisplacementMap in="SourceGraphic" in2="noise" scale="3" result="rough" />
            <feComposite in="rough" in2="speckle" operator="in" />
          </filter>
          {/* The banner covers the rings it crosses. */}
          <mask id="seal-banner">
            <rect x="-16" y="-16" width="232" height="232" fill="white" />
            <rect x="-14" y="72" width="228" height="56" fill="black" />
          </mask>
        </defs>
        <g filter="url(#seal-worn)" fill="currentColor" stroke="currentColor" transform="rotate(-15 100 100)">
          <g mask="url(#seal-banner)" fill="none">
            <circle cx="100" cy="100" r="94" strokeWidth="6" />
            <circle cx="100" cy="100" r="85" strokeWidth="1.5" />
            <circle cx="100" cy="100" r="70" strokeWidth="2" />
            <g fill="currentColor" stroke="none">
              {stars(52)}
              {stars(148)}
            </g>
          </g>
          <rect x="-10" y="76" width="220" height="48" fill="none" strokeWidth="3.5" />
          <rect x="-4" y="81" width="208" height="38" fill="none" strokeWidth="1.2" />
          <text x="100" y="101" textAnchor="middle" dominantBaseline="central" fontSize="24" fontWeight="700" textLength="184" lengthAdjust="spacingAndGlyphs" stroke="none">
            SPRINT READY
          </text>
        </g>
      </svg>
    </div>
  )
}

// Results shows what will be written to Jira. Everything on the original is
// editable in place, and the split tickets' titles; the points follow the
// ratings.
export function Results({ view, readOnly }: { view: View; readOnly: boolean }) {
  const refine = useRefine()
  const r = view.refinement
  const key = r.key
  const stamped = r.step === 'stamped'
  const locked = readOnly || stamped || (r.stamp.done ?? []).length > 0
  const children = Object.values(r.stamp.children ?? {})
  const [result, setResult] = useState<Result>(() => r.result!)
  const [saved, setSaved] = useState(true)
  const [stamping, setStamping] = useState(!!r.stamp.error)
  // slam counts the stamps landed here; each one animates the seal in.
  const [slam, setSlam] = useState(0)
  const timer = useRef<number>(undefined)

  const regenerate = async () => {
    await flush()
    void refine.run('rewrite', key)
  }

  const change = (next: Result) => {
    setResult(next)
    setSaved(false)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(async () => {
      const after = await refine.act(key, 'edit', () => api.edit(key, next))
      if (after) setSaved(true)
    }, 500)
  }
  const flush = async () => {
    window.clearTimeout(timer.current)
    if (!saved) {
      await refine.act(key, 'edit', () => api.edit(key, result))
      setSaved(true)
    }
  }

  const review = async () => {
    await flush()
    setStamping(true)
  }

  useEffect(() => {
    if (readOnly || stamped) return
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey && e.key.toLowerCase() === 's') {
        e.preventDefault()
        if (!refine.pending) void review()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  const s = score(result.ratings)
  const split = result.split ?? []
  const repos = result.technical_implementation ?? []

  return (
    <>
      <div className="results-body">
        {stamped && <Seal key={slam} slam={slam > 0} />}
        <div className="scroll">
          <div key={slam} className={`page${slam ? ' slam' : ''}`}>
            <div className="page-main">
              <section className={`split-card${split.length ? ' is-split' : ''}`} aria-label="Split">
                <div className="section-head">
                  <h2>{split.length ? `Split into ${split.length + 1} tickets` : 'Not split'}</h2>
                  {split.length > 0 && <span className="chip accent">{split.length} new</span>}
                </div>
                {result.split_reason && <p className="split-reason">{result.split_reason}</p>}
                <div className="row">
                  <span className="mono muted" style={{ width: 82 }}>
                    {key}
                  </span>
                  <span className="small">{split.length ? 'keeps the first slice' : 'is one actionable ticket'}</span>
                </div>
                {split.map((c, i) => (
                  <div key={i} className="stack" style={{ gap: 2 }}>
                    <div className="row">
                      <span className="chip accent" style={{ width: 82, justifyContent: 'center' }}>
                        {r.stamp.children?.[String(i)] ?? 'New'}
                      </span>
                      <textarea
                        className="inline-edit grow"
                        rows={1}
                        value={c.title}
                        readOnly={locked}
                        aria-label={`Split ticket ${i + 1} title`}
                        onChange={(e) => change({ ...result, split: split.map((x, j) => (j === i ? { ...x, title: e.target.value } : x)) })}
                      />
                    </div>
                    <span className="muted small" style={{ paddingLeft: 90 }}>
                      {c.points} point{c.points === 1 ? '' : 's'} · {(c.acceptance_criteria ?? []).length} criteria
                    </span>
                  </div>
                ))}
                {split.length > 0 && (
                  <p className="muted small" style={{ margin: 0 }}>
                    New tickets go to {refine.info?.ready_sprint} and relate to {key}. The fields below are {key}&apos;s first slice.
                  </p>
                )}
              </section>

              <section className="section">
                <div className="section-head">
                  <h2>Acceptance Criteria</h2>
                  {!locked && <span className="muted small">Click any line to edit. Enter adds a line.</span>}
                </div>
                {!locked && <FormatPicker field="ac" />}
                <Lines
                  lines={result.acceptance_criteria ?? []}
                  readOnly={locked}
                  placeholder="Criterion"
                  onChange={(lines) => change({ ...result, acceptance_criteria: lines })}
                />
              </section>

              <section className="section">
                <div className="section-head">
                  <h2>Technical Implementation</h2>
                  {!locked && <span className="muted small">One path per line; mark new files with (new). Decisions go below the ---.</span>}
                </div>
                {!locked && <FormatPicker field="ti" />}
                <div className="files">
                  {repos.map((repo, i) => (
                    <div key={repo.repo + i} className="stack" style={{ gap: 2 }}>
                      <span className="repo">{repo.repo}</span>
                      <textarea
                        className="inline-edit mono"
                        value={filesText(repo)}
                        readOnly={locked}
                        aria-label={`Files in ${repo.repo}`}
                        onChange={(e) =>
                          change({
                            ...result,
                            technical_implementation: repos.map((x, j) => (j === i ? { ...x, files: parseFiles(e.target.value) } : x))
                          })
                        }
                      />
                    </div>
                  ))}
                  {repos.length === 0 && <span className="muted">No files.</span>}
                  <span className="rule" aria-hidden="true">
                    ---
                  </span>
                  <Lines
                    lines={result.decisions ?? []}
                    readOnly={locked}
                    placeholder="Decision"
                    onChange={(lines) => change({ ...result, decisions: lines })}
                  />
                </div>
              </section>

              <section className="section">
                <div className="section-head">
                  <h2>Story points</h2>
                  <span style={{ fontWeight: 500 }}>{s ? `${s.points} point${s.points === 1 ? '' : 's'}` : 'Not rated'}</span>
                  {s && <span className="muted small">average {s.average.toFixed(1)}, worked out from the ratings</span>}
                </div>
                <div className="ratings">
                  {rows.map(({ key: dim, label }) => (
                    <div key={dim} className="rating-row">
                      <span className="grow" style={{ color: 'var(--text-3)' }}>
                        {label}
                      </span>
                      <div role="group" aria-label={label} className="segmented">
                        {((dim === 'testing' ? ['N/A', 'Low', 'Medium', 'High'] : ['Low', 'Medium', 'High']) as Rating[]).map((v) => (
                          <button
                            key={v}
                            aria-pressed={result.ratings[dim] === v}
                            disabled={locked}
                            onClick={() => change({ ...result, ratings: { ...result.ratings, [dim]: v } })}
                          >
                            {v}
                          </button>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
              </section>
            </div>

            <aside className="page-side">
              <div className="card">
                <h3>Deploy needs, posted as one comment</h3>
                <Lines
                  lines={result.deploy_needs ?? []}
                  readOnly={locked}
                  placeholder="Deploy need"
                  onChange={(lines) => change({ ...result, deploy_needs: lines })}
                />
              </div>

              {(r.related.blockers ?? []).some((b) => b.decision === 'accepted') && (
                <div className="card">
                  <h3>Links written at stamp</h3>
                  {(r.related.blockers ?? [])
                    .filter((b) => b.decision === 'accepted')
                    .map((b) => (
                      <span key={b.key} className="small">
                        <span className="muted">relates to</span> <span className="mono">{b.key}</span> {b.summary}
                      </span>
                    ))}
                </div>
              )}
            </aside>
          </div>
        </div>
      </div>
      {!readOnly && stamped && (
        <footer className="footbar">
          <span className="chip green">✓ Stamped</span>
          <span className="muted small">
            Written to Jira and moved to {refine.info?.ready_sprint}.{children.length > 0 && ` Split tickets: ${children.join(', ')}.`}
          </span>
          {/* TODO: temporary, to check the seal animation. */}
          <button className="btn" style={{ marginLeft: 'auto' }} onClick={() => setSlam((n) => n + 1)}>
            Replay stamp
          </button>
          <button
            className="btn primary"
            onClick={() => {
              refine.setScreen({ kind: 'tickets' })
              void refine.refresh()
            }}
          >
            Back to tickets
          </button>
        </footer>
      )}
      {!readOnly && !stamped && (
        <footer className="footbar">
          <span className="muted small">{refine.pending === 'edit' || !saved ? 'Saving…' : locked ? 'Stamp in progress' : 'Edits saved'}</span>
          <button
            className="btn"
            style={{ marginLeft: 'auto' }}
            disabled={!!refine.pending || locked}
            title="Rewrites the Acceptance Criteria and Technical Implementation with the current format, for this ticket and its split tickets. Your edits to those fields are replaced."
            onClick={() => void regenerate()}
          >
            Regenerate AC &amp; TI
          </button>
          <button
            className="btn primary"
            disabled={!!refine.pending}
            onClick={() => void review()}
          >
            Review and stamp <kbd>⌘S</kbd>
          </button>
        </footer>
      )}
      {stamping && <StampDialog view={view} onClose={() => setStamping(false)} onStamped={() => setSlam((n) => n + 1)} />}
    </>
  )
}
