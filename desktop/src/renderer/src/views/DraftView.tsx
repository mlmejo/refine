import { useState, type ReactNode } from 'react'
import { Check, Copy, FileCode, FolderOpen, Gauge, ListChecks, MessagesSquare, OctagonAlert, PenLine, Split, Stamp, Upload } from 'lucide-react'
import { api } from '../api'
import { useRefine } from '../store'
import { plural, questions, stamped, tokens, unanswered, type Anchor, type Child, type RepoFiles, type Saved, type StoryPointCriteria } from '../types'
import { Bullets, Card, Relations } from './Cards'
import { Md, TermsProvider } from './Md'
import { TicketContext } from './TicketContext'
import { ClearCard, DecisionDeck, DecisionSummary, decisions } from './DecisionDeck'

type Ask = 'write' | undefined

export function DraftView({ issueKey, busy }: { issueKey: string; busy: boolean }) {
  const refine = useRefine()
  const saved = refine.ticket(issueKey).saved!.draft!
  const d = saved.draft
  const [ask, setAsk] = useState<Ask>()
  const [writing, setWriting] = useState(false)
  const [note, setNote] = useState<{ text: string; error?: boolean }>()
  const [openChild, setOpenChild] = useState<number>()
  // panel is the ticket shown beside the deck, at an anchor's quote.
  const [panel, setPanel] = useState<{ anchor: Anchor | null }>()

  const left = unanswered(saved)
  const decisionCount = decisions(saved).length
  // Findings no question rests on are background; with no evidence at all
  // (drafts from before it), every finding is.
  const cited = new Set((d.open_questions ?? []).flatMap((q) => q.evidence ?? []))
  const background = (d.findings ?? []).filter((_, i) => !cited.has(i))
  const isStamped = stamped(saved)
  const legacy = !saved.step

  const fail = (text: string) => setNote({ text, error: true })

  const nextRound = () => {
    if (left > 0) return fail(`answer ${plural(left, 'more question')} before redrafting`)
    void refine.nextRound(issueKey)
  }
  // Stamping again needs the questions clear, as the first stamp did.
  const stampAgain = () => {
    if (questions(d).length > 0) return fail('Claude still has questions: run more questions until a round comes back with none')
    void refine.stamp(issueKey)
  }
  const askWrite = () => {
    if (!isStamped) return fail('stamp the ticket before writing to Jira')
    if (legacy && left > 0) return fail(`answer ${plural(left, 'more question')} and redraft before writing to Jira`)
    setNote(undefined)
    refine.track('write_confirm', issueKey, { checked: !!d.checked, duplicates: d.duplicates?.length ?? 0, open: left })
    setAsk('write')
  }
  const write = async () => {
    setAsk(undefined)
    setWriting(true)
    try {
      const out = await api.write(issueKey, saved.id)
      if (out.children) refine.setDraft(issueKey, { ...saved, children: out.children })
      refine.replaceIssue(out.issue)
      let written = `written to ${issueKey}`
      const keys = Object.entries(out.children ?? {})
        .sort(([a], [b]) => Number(a) - Number(b))
        .map(([, k]) => k)
      if (keys.length) written += `, split into ${keys.join(', ')}`
      if (out.split_error) {
        fail(`${written}, but could not create every split ticket, so it stays here; write again creates the rest: ${out.split_error}`)
      } else if (out.move_error) {
        fail(`${written}, but could not move it to ${refine.info?.ready_sprint}: ${out.move_error}`)
      } else if (out.moved) {
        // It has left the refinement sprint, so it leaves the list.
        refine.setFlash({ text: `${written} and moved to ${refine.info?.ready_sprint}` })
        refine.dropIssue(issueKey)
      } else {
        setNote({ text: written })
      }
    } catch (err) {
      fail((err as Error).message)
    } finally {
      setWriting(false)
    }
  }
  const reveal = async () => {
    refine.track('markdown', issueKey)
    try {
      const out = await api.reveal(issueKey)
      setNote({ text: out.path })
    } catch (err) {
      fail((err as Error).message)
    }
  }

  // Arm B of the plain-names experiment shows plain names with the code on
  // hover; arm A and drafts outside the experiment render as before.
  const terms = d.arm === 'B' ? (d.terms ?? []) : []
  return (
    <TermsProvider
      terms={terms}
      onHover={(name) => refine.track('term_hover', issueKey, { arm: d.arm ?? '', term_chars: name.length })}
    >
    <div className={(openChild !== undefined && d.split?.[openChild]) || panel ? 'split' : ''}>
      <div className="stack">
        <Meta saved={saved} />
        {isStamped && !legacy && <Confidence issueKey={issueKey} saved={saved} />}

        <div className="actions sticky">
          {!isStamped && legacy && (
            <button disabled={busy} onClick={nextRound} title="Continue the session with your answers">
              Redraft with answers
            </button>
          )}
          {isStamped && (
            <>
              <button className="primary" disabled={busy || writing} onClick={askWrite}>
                <Upload aria-hidden />
                {writing ? `Writing to ${issueKey}…` : 'Write to Jira'}
              </button>
              <button disabled={busy} onClick={nextRound}>
                <MessagesSquare aria-hidden />
                {legacy ? 'Redraft with answers' : 'More questions'}
              </button>
              {!legacy && (
                <button disabled={busy} onClick={stampAgain}>
                  <Stamp aria-hidden />
                  Stamp again
                </button>
              )}
            </>
          )}
          <button onClick={() => refine.setTab(issueKey, 'approach')}>
            <PenLine aria-hidden />
            Redo approach
          </button>
          {saved.markdown && (
            <button onClick={() => void reveal()}>
              <FolderOpen aria-hidden />
              Markdown
            </button>
          )}
        </div>

        {ask === 'write' && (
          <WriteConfirm
            saved={saved}
            onYes={() => void write()}
            onNo={() => {
              refine.track('write_cancel', issueKey)
              setAsk(undefined)
            }}
          />
        )}
        {note && <div className={`banner ${note.error ? 'error' : 'done'}`}>{note.text}</div>}

        {!isStamped && !legacy && questions(d).length > 0 && <DecisionDeck issueKey={issueKey} saved={saved} busy={busy} onShowTicket={(anchor) => { setOpenChild(undefined); setPanel({ anchor }) }} />}
        {!isStamped && !legacy && questions(d).length === 0 && <ClearCard issueKey={issueKey} saved={saved} busy={busy} />}
        {isStamped && (
          <>
            <Card title={`Acceptance Criteria · ${d.acceptance_criteria?.length ?? 0}`} icon={ListChecks}>
              <Bullets items={d.acceptance_criteria} marker="*" />
            </Card>
            <Card title="Technical Implementation" icon={FileCode}>
              <Repos repos={d.technical_implementation} />
            </Card>
            <Card title="Story Point Criteria" icon={Gauge}>
              <Points spc={d.story_point_criteria} points={d.points} />
            </Card>
            {decisionCount > 0 && (
              <Fold issueKey={issueKey} title={`Your decisions · ${decisionCount}`}>
                <DecisionSummary saved={saved} />
              </Fold>
            )}
          </>
        )}
        {d.duplicates && d.duplicates.length > 0 && (
          <Card title={`Already delivered by other tickets · ${d.duplicates.length}`} tone="warn" icon={Copy}>
            {d.duplicates.map((dup, i) => (
              <div key={i} className="dup">
                <div className="error-text">
                  {dup.ticket} · delivered by {dup.key}
                </div>
                <div className="indent">
                  “<Md text={dup.criterion} />”, already “<Md text={dup.covered} />”
                </div>
              </div>
            ))}
          </Card>
        )}
        {d.split && d.split.length > 0 && (
          <Card title={`Split · ${plural(d.split.length, 'ticket')}`} icon={Split}>
            {d.split.map((child, i) => {
              const created = saved.children?.[String(i)]
              return (
                <button
                  key={i}
                  className={`child-row ${openChild === i ? 'active' : ''}`}
                  onClick={() => setOpenChild(openChild === i ? undefined : i)}
                >
                  ↗ {child.title} · {child.points} {child.points === 1 ? 'pt' : 'pts'}
                  {created && <span className="key"> · {created}</span>}
                </button>
              )
            })}
          </Card>
        )}

        <ApproachReview issueKey={issueKey} saved={saved} />
        {background.length > 0 && (
          <Fold issueKey={issueKey} title={`Background · ${plural(background.length, 'finding')}`}>
            <Bullets items={background} />
          </Fold>
        )}
        {d.problem && (
          <Fold issueKey={issueKey} title="Ticket">
            <p>
              <strong>Problem:</strong> <Md text={d.problem} />
            </p>
            <p>
              <strong>Solution:</strong> <Md text={d.solution} />
            </p>
            <h4>Blockers</h4>
            <Bullets items={d.ticket_blockers} marker={<OctagonAlert className="tone-err" aria-hidden />} />
          </Fold>
        )}
        <Fold issueKey={issueKey} title="Deployment">
          <p>
            <strong>Platform:</strong> <Md text={d.deployment.platform} />
          </p>
          <h4>Evidence</h4>
          <Bullets items={d.deployment.evidence} />
          <h4>Steps</h4>
          <Bullets items={d.deployment.steps} />
        </Fold>
        {d.elsewhere && d.elsewhere.length > 0 && (
          <Fold issueKey={issueKey} title={`Left to other tickets · ${d.elsewhere.length}`}>
            {d.elsewhere.map((item, i) => (
              <div key={i}>
                <span className="key">{item.key}</span>
                <div className="indent">
                  <Md text={item.behaviour} />
                </div>
              </div>
            ))}
          </Fold>
        )}
        {d.relations && d.relations.length > 0 && (
          <Fold issueKey={issueKey} title={`Related tickets · ${d.relations.length}`}>
            <Relations relations={d.relations} />
          </Fold>
        )}
      </div>
      {panel && !(openChild !== undefined && d.split?.[openChild]) && (
        <div className="side">
          <div className="side-head">
            <strong>{issueKey} in Jira</strong>
            <button className="link push" onClick={() => setPanel(undefined)}>
              close
            </button>
          </div>
          <TicketContext issueKey={issueKey} anchor={panel.anchor} />
        </div>
      )}
      {openChild !== undefined && d.split?.[openChild] && (
        <div className="side">
          <ChildCard child={d.split[openChild]} created={saved.children?.[String(openChild)]} onClose={() => setOpenChild(undefined)} />
        </div>
      )}
    </div>
    </TermsProvider>
  )
}

// Confidence asks once per stamped draft how sure the developer is that the
// ticket is right: a measure for the plain-names experiment.
function Confidence({ issueKey, saved }: { issueKey: string; saved: Saved }) {
  const refine = useRefine()
  const storeKey = `confidence-${saved.id}`
  const [value, setValue] = useState<number | null>(() => {
    try {
      const v = localStorage.getItem(storeKey)
      return v ? Number(v) : null
    } catch {
      return null
    }
  })
  if (!refine.info?.tracking) return null
  const pick = (n: number) => {
    setValue(n)
    try {
      localStorage.setItem(storeKey, String(n))
    } catch {
      // Asking again next time is fine.
    }
    refine.track('confidence', issueKey, { value: n, arm: saved.draft.arm ?? '', draft: saved.id })
  }
  return (
    <div className="confidence">
      <span>How sure are you this ticket is right?</span>
      {[1, 2, 3, 4, 5].map((n) => (
        <button key={n} className={value === n ? 'picked' : ''} onClick={() => pick(n)}>
          {n}
        </button>
      ))}
      <span className="quiet small">{value ? 'thanks' : '1 not sure · 5 sure'}</span>
    </div>
  )
}

function Meta({ saved }: { saved: Saved }) {
  const d = saved.draft
  const when = new Date(saved.created).toLocaleString()
  const parts: string[] = []
  if (saved.step === 'questions') parts.push('questions')
  else parts.push(saved.step === 'stamp' ? 'stamped' : 'draft', `${d.points} pts`)
  parts.push(`$${saved.cost_usd.toFixed(2)}`, when)
  if (saved.ref) parts.push(`repos at ${saved.ref}`)
  if (saved.turns) {
    const u = saved.usage
    parts.push(
      `${saved.turns} turns`,
      `${tokens(u.input_tokens + u.cache_creation_input_tokens + u.cache_read_input_tokens)} in`,
      `${tokens(u.output_tokens)} out`
    )
  }
  const total = questions(d).length
  if (total > 0) parts.push(`${total - unanswered(saved)}/${total} answered`)
  return <div className="quiet small">{parts.join(' · ')}</div>
}

// Fold is a section folded away by default: context to open when needed,
// not to read first.
function Fold({ issueKey, title, children }: { issueKey: string; title: string; children: ReactNode }) {
  const refine = useRefine()
  return (
    <details
      className="fold"
      onToggle={(e) => {
        if ((e.target as HTMLDetailsElement).open) refine.track('fold_open', issueKey, { section: title.split(' · ')[0] })
      }}
    >
      <summary>{title}</summary>
      <div className="fold-body">{children}</div>
    </details>
  )
}

// ApproachReview shows what needs attention in the developer's approach:
// what it misses, what it was unsure of, and the alternatives. What holds is
// folded to a count.
function ApproachReview({ issueKey, saved }: { issueKey: string; saved: Saved }) {
  const refine = useRefine()
  const r = saved.draft.approach_review
  const holds = r.holds ?? []
  const parts: [string, string[] | null][] = [
    ['Not covered yet', r.misses],
    ['Unsure, answered', r.unsure],
    ['Alternatives', r.alternatives]
  ]
  return (
    <Card title="Your approach" icon={PenLine}>
      {saved.approach.trim() ? <p className="prewrap">{saved.approach}</p> : <div className="quiet italic">you didn't know yet</div>}
      {parts
        .filter(([, items]) => (items ?? []).length > 0)
        .map(([name, items]) => (
          <div key={name}>
            <h4>{name}</h4>
            <Bullets items={items} />
          </div>
        ))}
      {holds.length > 0 && (
        <details
          className="holds"
          onToggle={(e) => {
            if ((e.target as HTMLDetailsElement).open) refine.track('fold_open', issueKey, { section: 'Holds' })
          }}
        >
          <summary>
            <Check className="tone-ok" aria-hidden /> {plural(holds.length, 'part')} of your approach hold
          </summary>
          <Bullets items={holds} />
        </details>
      )}
    </Card>
  )
}

function WriteConfirm({ saved, onYes, onNo }: { saved: Saved; onYes: () => void; onNo: () => void }) {
  const d = saved.draft
  const splitLeft = (d.split ?? []).filter((_, i) => !saved.children?.[String(i)]).length
  let warning: string | undefined
  if (!d.checked) warning = 'Not checked against other open tickets.'
  else if (d.duplicates?.length === 1) warning = '1 criterion is already delivered by another ticket.'
  else if (d.duplicates && d.duplicates.length > 1)
    warning = `${d.duplicates.length} criteria are already delivered by other tickets.`
  return (
    <div className="confirm">
      {warning && <p className="error-text">{warning}</p>}
      <p>
        Replace {saved.key}'s Acceptance Criteria, Technical Implementation, Story Point Criteria and points ({d.points})
        {splitLeft > 0 && `, create ${plural(splitLeft, 'split ticket')}`}, then move it to the ready sprint?
      </p>
      <div className="actions">
        <button className="primary" onClick={onYes}>
          Write to Jira
        </button>
        <button onClick={onNo}>Cancel</button>
      </div>
    </div>
  )
}

function Repos({ repos }: { repos: RepoFiles[] | null }) {
  if (!repos || repos.length === 0) return <div className="quiet italic">none</div>
  return (
    <div>
      {repos.map((repo, i) => (
        <div key={i}>
          <div className="key">{repo.repo}</div>
          {(repo.files ?? []).map((file, j) => (
            <div key={j} className="indent mono">
              {file.path}
              {file.new && <span className="quiet"> (new)</span>}
            </div>
          ))}
        </div>
      ))}
    </div>
  )
}

const spcRows: [keyof StoryPointCriteria, string][] = [
  ['novelty', 'Novelty & Complexity of Implementation'],
  ['risk', 'Risk & Feature Impact'],
  ['dependency', 'Dependency & Integration Complexity'],
  ['testing', 'Testing & QA Effort'],
  ['deployment', 'Deployment Complexity'],
  ['total', 'Total']
]

function Points({ spc, points }: { spc: StoryPointCriteria; points?: number }) {
  return (
    <table className="spc">
      <tbody>
        {spcRows.map(([field, label]) => (
          <tr key={field}>
            <td>{label}</td>
            <td>{spc[field]}</td>
          </tr>
        ))}
        {points !== undefined && (
          <tr className="total">
            <td>Points</td>
            <td>{points}</td>
          </tr>
        )}
      </tbody>
    </table>
  )
}

function rated(spc: StoryPointCriteria): boolean {
  return spcRows.some(([field]) => spc[field] !== '')
}

function ChildCard({ child, created, onClose }: { child: Child; created?: string; onClose: () => void }) {
  return (
    <Card
      title={
        <span className="card-title-row">
          {child.title}
          <button className="link push" onClick={onClose}>
            close
          </button>
        </span>
      }
    >
      <p>
        <strong>
          {child.points} {child.points === 1 ? 'pt' : 'pts'}
          {created && ` · created as ${created}`}
        </strong>
      </p>
      <h4>Acceptance Criteria</h4>
      <Bullets items={child.acceptance_criteria} marker="*" />
      <h4>Technical Implementation</h4>
      <Repos repos={child.technical_implementation} />
      {rated(child.story_point_criteria) && (
        <>
          <h4>Story Point Criteria</h4>
          <Points spc={child.story_point_criteria} />
        </>
      )}
    </Card>
  )
}
