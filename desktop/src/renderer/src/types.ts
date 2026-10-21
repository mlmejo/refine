// These mirror the JSON internal/server sends.

export type Info = {
  title: string
  site: string
  project: string
  ready_sprint: string
  sync: boolean
  // tracking is the usage-tracking flag; events are sent only when it is on.
  tracking: boolean
}

export type Issue = {
  key: string
  summary: string
  status: string
  acceptance_criteria: string
  technical_implementation: string
  story_point_criteria: string
}

export type Usage = {
  input_tokens: number
  cache_creation_input_tokens: number
  cache_read_input_tokens: number
  output_tokens: number
}

export type Relation = { key: string; kind: string; why: string }

// Choice is a question answered by picking an option or in words. options is
// empty for an open question, whose recommended is a suggested answer.
// Answers are keyed by question.
export type Choice = {
  question: string
  options: string[] | null
  recommended: string
  why: string
}

// ScopeQuestion asks what to build; the developer answers it with their
// approach.
export type ScopeQuestion = Choice

export type Triage = {
  problem: string
  solution: string
  blockers: string[] | null
  scope_questions: ScopeQuestion[] | null
  relations: Relation[] | null
}

type Run = {
  id: number
  session_id: string
  cost_usd: number
  turns: number
  usage: Usage
  created: string
}

export type SavedTriage = Run & { triage: Triage }

export type Brief = {
  summary: string
  flow: string[] | null
  parts: { repo: string; path: string; role: string }[] | null
  terms: { term: string; meaning: string }[] | null
  gotchas: string[] | null
  ask_team: string[] | null
}

export type SavedBrief = Run & { ref?: string; brief: Brief }

export type RepoFiles = { repo: string; files: { path: string; new: boolean }[] | null }

export type StoryPointCriteria = {
  novelty: string
  risk: string
  dependency: string
  testing: string
  deployment: string
  total: string
}

export type Child = {
  title: string
  points: number
  acceptance_criteria: string[] | null
  technical_implementation: RepoFiles[] | null
  story_point_criteria: StoryPointCriteria
}

export type Draft = {
  problem?: string
  solution?: string
  ticket_blockers?: string[] | null
  findings?: string[] | null
  acceptance_criteria: string[] | null
  technical_implementation: RepoFiles[] | null
  story_point_criteria: StoryPointCriteria
  points: number
  deployment: { platform: string; evidence: string[] | null; steps: string[] | null; blockers: string[] | null }
  split: Child[] | null
  elsewhere?: { key: string; behaviour: string }[] | null
  relations: Relation[] | null
  approach_review: {
    holds: string[] | null
    unsure: string[] | null
    misses: string[] | null
    alternatives: string[] | null
  }
  open_questions: Choice[] | null
  checked?: boolean
  duplicates?: { ticket: string; criterion: string; key: string; covered: string }[] | null
}

export type Saved = {
  id: number
  key: string
  approach: string
  ref?: string
  step?: '' | 'questions' | 'stamp'
  session_id: string
  cost_usd: number
  session_cost_usd?: number
  turns?: number
  usage: Usage
  created: string
  draft: Draft
  markdown?: string
  answers?: Record<string, string> | null
  scope?: Record<string, string> | null
  children?: Record<string, string> | null
}

export type SavedSet = {
  draft: Saved | null
  brief: SavedBrief | null
  triage: SavedTriage | null
}

export type JobKind = 'triage' | 'brief' | 'draft' | 'stamp'

export type Job = {
  id: string
  kind: JobKind
  key: string
  resume?: boolean
  from_brief?: boolean
  started: string
  done: boolean
  error?: string
  triage?: SavedTriage
  brief?: SavedBrief
  draft?: Saved
}

export type RepoStatus = {
  name: string
  dir: string
  branch: string
  behind: number
  origin: string
  skip?: string
}

export type Repo = { name: string; dir: string }

// Found is a git checkout under a scanned folder; listed is the name it is
// configured under already.
export type Found = { name: string; dir: string; listed?: string }

export type Written = {
  issue: Issue
  children?: Record<string, string>
  split_error?: string
  move_error?: string
  moved: boolean
}

// questions lists a draft's questions in order: deployment blockers, then open
// questions, as draft.Draft.Questions does.
export function questions(d: Draft): string[] {
  return [...(d.deployment.blockers ?? []), ...(d.open_questions ?? []).map((q) => q.question)]
}

export function unanswered(s: Saved): number {
  return questions(s.draft).filter((q) => !(s.answers?.[q] ?? '').trim()).length
}

// stamped matches draft.Saved.Stamped: everything but the questions step.
export function stamped(s: Saved): boolean {
  return s.step !== 'questions'
}

export function refined(i: Issue): boolean {
  return i.acceptance_criteria !== '' && i.technical_implementation !== ''
}

export function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? '' : 's'}`
}

export function tokens(n: number): string {
  return n < 1000 ? String(n) : `${Math.round(n / 1000)}K`
}
