import { useEffect, useRef, useState } from 'react'
import type { Question } from '../../types'
import { typing } from '../../typing'

// QuestionCard is one multiple-choice question. The recommended option is
// highlighted; "Other answer…" takes the developer's own words.
export function QuestionCard({
  n,
  question,
  answer,
  focused,
  readOnly,
  onFocus,
  onAnswer
}: {
  n: number
  question: Question
  answer: string
  focused: boolean
  readOnly: boolean
  onFocus: () => void
  onAnswer: (answer: string) => void
}) {
  const options = question.options ?? []
  const own = answer !== '' && !options.includes(answer)
  const [writing, setWriting] = useState(own)
  const box = useRef<HTMLTextAreaElement>(null)
  useEffect(() => {
    if (writing && focused) box.current?.focus()
  }, [writing, focused])

  return (
    <div className={`question ${focused && !readOnly ? 'focused' : ''}`} onClick={onFocus}>
      <div className="q">
        <span className="n">{n}</span>
        <span>{question.text}</span>
      </div>
      <div role="radiogroup" aria-label={question.text} className="options">
        {options.map((option, i) => {
          const picked = answer === option
          const recommended = i === question.recommended
          return (
            <button
              key={option}
              role="radio"
              aria-checked={picked}
              disabled={readOnly}
              className={`option ${recommended ? 'recommended' : ''} ${picked ? 'picked' : ''}`}
              onClick={() => {
                setWriting(false)
                onAnswer(option)
              }}
            >
              <span className="n mono muted small">{i + 1}</span>
              <span className="radio" />
              <span className="grow">{option}</span>
              {recommended && <span className="chip accent">Recommended</span>}
            </button>
          )
        })}
        {writing || own ? (
          <div className={`other ${own ? 'picked' : ''}`}>
            <textarea
              ref={box}
              rows={2}
              aria-label="Your own answer"
              placeholder="Your own answer"
              disabled={readOnly}
              value={own ? answer : ''}
              onChange={(e) => onAnswer(e.target.value)}
            />
          </div>
        ) : (
          !readOnly && (
            <button className="link" style={{ alignSelf: 'flex-start', color: 'var(--muted)', padding: '4px 0' }} onClick={() => setWriting(true)}>
              Other answer…
            </button>
          )
        )}
        {question.why && focused && <span className="muted small">Recommended because {lower(question.why)}</span>}
      </div>
    </div>
  )
}

function lower(s: string): string {
  return s ? s[0].toLowerCase() + s.slice(1) : s
}

// useAnswers keeps answers as they are typed and saves them a moment after
// the last change.
export function useAnswers(initial: string[], save: (answers: string[]) => Promise<unknown>) {
  const [answers, setAnswers] = useState(initial)
  const timer = useRef<number>(undefined)
  const latest = useRef(answers)
  const set = (i: number, value: string) => {
    const next = answers.slice()
    next[i] = value
    setAnswers(next)
    latest.current = next
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => void save(next), 400)
  }
  // flush saves now, before a step that needs every answer stored.
  const flush = async () => {
    window.clearTimeout(timer.current)
    await save(latest.current)
  }
  return { answers, set, flush }
}

// useQuestionKeys picks options with 1–4 and the recommended one with R on
// the focused question, then moves on to the next unanswered one.
export function useQuestionKeys(
  questions: Question[],
  answers: string[],
  focus: number,
  setFocus: (i: number) => void,
  answer: (i: number, value: string) => void,
  submit: () => void,
  active: boolean
) {
  useEffect(() => {
    if (!active) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Enter' && e.metaKey) {
        e.preventDefault()
        submit()
        return
      }
      if (typing(e) || e.metaKey || e.ctrlKey) return
      const q = questions[focus]
      if (!q) return
      let pick: string | undefined
      if (e.key === 'r') pick = q.options[q.recommended]
      else if (/^[1-4]$/.test(e.key)) pick = q.options[Number(e.key) - 1]
      else if (e.key === 'j') setFocus(Math.min(focus + 1, questions.length - 1))
      else if (e.key === 'k') setFocus(Math.max(focus - 1, 0))
      else return
      e.preventDefault()
      if (pick === undefined) return
      answer(focus, pick)
      const nextOpen = questions.findIndex((_, i) => i > focus && !answers[i])
      if (nextOpen >= 0) setFocus(nextOpen)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })
}
