import { useState } from 'react'
import { api } from '../api'
import { useRefine } from '../store'
import { plural, questions, stamped, tokens, unanswered, type Child, type Choice, type RepoFiles, type Saved, type StoryPointCriteria } from '../types'
import { Bullets, Card, Relations } from './Cards'
import { Md } from './Md'

type Ask = 'stamp' | 'write' | undefined

export function DraftView({ issueKey, busy }: { issueKey: string; busy: boolean }) {
  const refine = useRefine()
  const saved = refine.ticket(issueKey).saved!.draft!
  const d = saved.draft
  const [ask, setAsk] = useState<Ask>()
  const [writing, setWriting] = useState(false)
  const [note, setNote] = useState<{ text: string; error?: boolean }>()
  const [openChild, setOpenChild] = useState<number>()

  const total = questions(d).length
  const left = unanswered(saved)
  const isStamped = stamped(saved)
  const legacy = !saved.step

  const fail = (text: string) => setNote({ text, error: true })

  const nextRound = () => {
    if (left > 0) return fail(`answer ${plural(left, 'more question')} before redrafting`)
    void refine.nextRound(issueKey)
  }
  const stamp = () => {
    setNote(undefined)
    if (left > 0) return setAsk('stamp')
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

  return (
    <div className={openChild !== undefined && d.split?.[openChild] ? 'split' : ''}>
      <div className="stack">
        <Meta saved={saved} />

        <div className="actions sticky">
          {!isStamped && (
            <>
              <button disabled={busy} onClick={nextRound} title="Continue the session with your answers">
                Next round with answers
              </button>
              {!legacy && (
                <button className="primary" disabled={busy} onClick={stamp}>
                  Stamp
                </button>
              )}
            </>
          )}
          {isStamped && (
            <>
              <button className="primary" disabled={busy || writing} onClick={askWrite}>
                {writing ? `Writing to ${issueKey}…` : 'Write to Jira'}
              </button>
              <button disabled={busy} onClick={nextRound}>
                {legacy ? 'Redraft with answers' : 'More questions'}
              </button>
              {!legacy && (
                <button disabled={busy} onClick={stamp}>
                  Stamp again
                </button>
              )}
            </>
          )}
          <button onClick={() => refine.setTab(issueKey, 'approach')}>Redo approach</button>
          {saved.markdown && <button onClick={() => void reveal()}>Markdown</button>}
        </div>

        {ask === 'stamp' && (
          <div className="confirm">
            <p>
              {plural(left, 'question')} still open. Stamp anyway? They stay in the draft as open, and you own
              proceeding without them.
            </p>
            <div className="actions">
              <button
                className="primary"
                onClick={() => {
                  setAsk(undefined)
                  void refine.stamp(issueKey)
                }}
              >
                Stamp anyway
              </button>
              <button onClick={() => setAsk(undefined)}>Cancel</button>
            </div>
          </div>
        )}
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

        {d.problem && (
          <Card title="Ticket">
            <p>
              <strong>Problem:</strong> <Md text={d.problem} />
            </p>
            <p>
              <strong>Solution:</strong> <Md text={d.solution} />
            </p>
            <h4>Blockers</h4>
            <Bullets items={d.ticket_blockers} marker="⛔" />
          </Card>
        )}
        <Card title="Your approach">
          {saved.approach.trim() ? <p className="prewrap">{saved.approach}</p> : <div className="quiet italic">you didn't know yet</div>}
        </Card>
        {((d.findings?.length ?? 0) > 0 || !isStamped) && (
          <Card title={`Findings · ${d.findings?.length ?? 0}`}>
            <Bullets items={d.findings} />
          </Card>
        )}
        <Card title="Approach review">
          <h4>Holds</h4>
          <Bullets items={d.approach_review.holds} />
          <h4>Unsure, answered</h4>
          <Bullets items={d.approach_review.unsure} />
          <h4>Not covered yet</h4>
          <Bullets items={d.approach_review.misses} />
          <h4>Alternatives</h4>
          <Bullets items={d.approach_review.alternatives} />
        </Card>
        {isStamped && (
          <>
            <Card title={`Acceptance Criteria · ${d.acceptance_criteria?.length ?? 0}`}>
              <Bullets items={d.acceptance_criteria} marker="*" />
            </Card>
            <Card title="Technical Implementation">
              <Repos repos={d.technical_implementation} />
            </Card>
            <Card title="Story Point Criteria">
              <Points spc={d.story_point_criteria} points={d.points} />
            </Card>
          </>
        )}
        <Card title="Deployment">
          <p>
            <strong>Platform:</strong> <Md text={d.deployment.platform} />
          </p>
          <h4>Evidence</h4>
          <Bullets items={d.deployment.evidence} />
          <h4>Steps</h4>
          <Bullets items={d.deployment.steps} />
          <h4>Blockers</h4>
          {(d.deployment.blockers ?? []).length === 0 && <div className="quiet italic">none</div>}
          {(d.deployment.blockers ?? []).map((q) => (
            <Question key={q} issueKey={issueKey} saved={saved} choice={{ question: q, options: null, recommended: '', why: '' }} />
          ))}
        </Card>
        {d.split && d.split.length > 0 && (
          <Card title={`Split · ${plural(d.split.length, 'ticket')}`}>
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
        {d.duplicates && d.duplicates.length > 0 && (
          <Card title={`Already delivered by other tickets · ${d.duplicates.length}`} tone="warn">
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
        {d.elsewhere && d.elsewhere.length > 0 && (
          <Card title={`Left to other tickets · ${d.elsewhere.length}`}>
            {d.elsewhere.map((item, i) => (
              <div key={i}>
                <span className="key">{item.key}</span>
                <div className="indent">
                  <Md text={item.behaviour} />
                </div>
              </div>
            ))}
          </Card>
        )}
        {d.relations && d.relations.length > 0 && (
          <Card title={`Related tickets · ${d.relations.length}`}>
            <Relations relations={d.relations} />
          </Card>
        )}
        <Card title={`Open questions · ${total - left}/${total} answered`}>
          {(d.open_questions ?? []).length === 0 && <div className="quiet italic">none</div>}
          {(d.open_questions ?? []).map((q) => (
            <Question key={q.question} issueKey={issueKey} saved={saved} choice={q} />
          ))}
        </Card>
      </div>
      {openChild !== undefined && d.split?.[openChild] && (
        <div className="side">
          <ChildCard child={d.split[openChild]} created={saved.children?.[String(openChild)]} onClose={() => setOpenChild(undefined)} />
        </div>
      )}
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

// Question shows one question with its answer. A question with options is
// answered by its radios, saved at once; any question can be answered in
// words, saved when the box loses focus or on enter.
function Question({ issueKey, saved, choice }: { issueKey: string; saved: Saved; choice: Choice }) {
  const refine = useRefine()
  const text = choice.question
  const options = choice.options ?? []
  const answer = saved.answers?.[text] ?? ''
  const own = answer.trim() !== '' && !options.includes(answer)
  const [value, setValue] = useState(own ? answer : '')
  const [editing, setEditing] = useState(false)
  const [error, setError] = useState<string>()
  const answered = answer.trim() !== ''

  const save = async (next: string) => {
    if (next === answer) return
    try {
      const updated = await api.answers(issueKey, saved.id, { ...(saved.answers ?? {}), [text]: next })
      refine.setDraft(issueKey, updated)
      setError(undefined)
    } catch (err) {
      setError(`could not save the answer: ${(err as Error).message}`)
    }
  }
  const saveWords = () => {
    setEditing(false)
    void save(value.trim())
  }

  return (
    <div className={`question ${answered ? 'answered' : ''}`}>
      <div className="q-text">
        <span className="q-state">{answered ? '✓' : '○'}</span> <Md text={text} />
      </div>
      {options.length > 0 && (
        <div className="q-options">
          {options.map((option) => (
            <label key={option} className="scope-option">
              <input type="radio" name={`q-${saved.id}-${text}`} checked={answer === option} onChange={() => void save(option)} />
              <span>
                <Md text={option} />
                {option === choice.recommended && (
                  <span className="star">
                    {' '}
                    ★ recommended{choice.why ? ` · ${choice.why}` : ''}
                  </span>
                )}
              </span>
            </label>
          ))}
        </div>
      )}
      {editing ? (
        <textarea
          autoFocus
          className="answer"
          value={value}
          placeholder={options.length > 0 ? 'Other answer' : 'Your answer, even just "no"'}
          onChange={(e) => setValue(e.target.value)}
          onBlur={saveWords}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault()
              saveWords()
            }
            if (e.key === 'Escape') {
              setValue(own ? answer : '')
              setEditing(false)
            }
          }}
        />
      ) : (
        <button className="answer-view" onClick={() => setEditing(true)}>
          {own ? (
            <span className="italic">
              ↳ <Md text={answer} />
            </span>
          ) : options.length > 0 ? (
            <span className="quiet italic">↳ other answer…</span>
          ) : answered ? null : saved.step === 'stamp' ? (
            <span className="quiet italic">↳ still open: stamped without an answer</span>
          ) : (
            <span className="quiet italic">
              ↳ needs an answer{choice.recommended ? ` · suggested: ${choice.recommended}` : ''}
            </span>
          )}
        </button>
      )}
      {options.length > 0 && !answered && saved.step === 'stamp' && (
        <div className="quiet italic small">still open: stamped without an answer</div>
      )}
      {answered && (
        <button className="link" onClick={() => void save('')}>
          clear
        </button>
      )}
      {error && <div className="error-text small">{error}</div>}
    </div>
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
