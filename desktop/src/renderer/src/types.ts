// These mirror the JSON the refine backend serves (internal/refine,
// internal/server). Go writes an empty list as null, so lists are optional.

export type Info = {
  site: string
  project: string
  sprint: string
  ready_sprint: string
  max_rounds: number
  max_questions: number
  build: string
  prompts: string
}

export type Row = {
  key: string
  summary: string
  status: string
  started: boolean
  step: string
  blocked_by: string[] | null
  blocks: string[] | null
  updated?: string
  points?: number | null
}

export type Page = { user: string; rows: Row[] | null; ready: Row[] | null }

export type Step = 'related' | 'repos' | 'questions' | 'deploy' | 'results' | 'stamped'

export type Blocker = {
  key: string
  summary: string
  reason: string
  quote: string
  refined: boolean
  decision?: 'accepted' | 'rejected' | ''
  from_code?: boolean
}

export type Overlap = { key: string; summary: string; reason: string }

export type Related = {
  searched: boolean
  explanations?: string[] | null
  blockers: Blocker[] | null
  overlaps: Overlap[] | null
  waiting?: string[] | null
}

export type Synced = { name: string; branch: string; commit: string; new: number; error?: string }

export type Question = {
  text: string
  options: string[]
  recommended: number
  why?: string
  answer?: string
}

export type RepoQuestion = Question & { repo: string }

export type Deploy = { repos: RepoQuestion[] | null; ticket: Question[] | null }

export type RepoFiles = { repo: string; files: { path: string; new: boolean }[] | null }

export type Rating = 'Low' | 'Medium' | 'High' | 'N/A' | ''

export type Ratings = {
  novelty: Rating
  risk: Rating
  dependency: Rating
  testing: Rating
  deployment: Rating
}

export type Child = {
  title: string
  acceptance_criteria: string[] | null
  technical_implementation: RepoFiles[] | null
  decisions: string[] | null
  ratings: Ratings
  points: number
}

export type Result = {
  acceptance_criteria: string[] | null
  technical_implementation: RepoFiles[] | null
  decisions: string[] | null
  ratings: Ratings
  points: number
  split: Child[] | null
  split_reason?: string
  deploy_needs: string[] | null
}

export type Stamping = {
  done?: string[] | null
  children?: Record<string, string> | null
  failed?: string
  error?: string
}

export type Refinement = {
  key: string
  summary: string
  step: Step
  related: Related
  repos: string[] | null
  synced: Synced[] | null
  suggestion?: { repos: SuggestedRepo[] | null } | null
  rounds: { questions: Question[] }[] | null
  deploy?: Deploy | null
  result?: Result | null
  stamp: Stamping
  cost_usd: number
  updated: string
}

export type Link = { type: string; relation: string; key: string; summary: string; status: string }

export type TicketInfo = {
  key: string
  summary: string
  status: string
  description_md: string
  links: Link[] | null
}

export type QA = { question: string; answer: string }
export type Profile = { repo: string; answers: QA[] | null; updated: string }

export type Action = { id: string; label: string }

export type View = {
  refinement: Refinement
  ticket: TicketInfo
  started: boolean
  suggested_repos: SuggestedRepo[] | null
  profiles: Profile[] | null
  stamp_plan: Action[] | null
}

export type Activity = { verb: string; what: string; at: string }

export type JobKind = 'search' | 'ask' | 'deploy' | 'generate' | 'rewrite'

// FormatField names a field whose rules the developer can upload:
// Acceptance Criteria or Technical Implementation.
export type FormatField = 'ac' | 'ti'

// Format is where a field's rules live, and whether they are the
// developer's own.
export type Format = { path: string; custom: boolean }

export type Job = {
  id: string
  kind: JobKind
  key: string
  started: string
  done: boolean
  error?: string
  log: Activity[] | null
}

export type SuggestedRepo = { repo: string; reason: string }

export type Repo = { name: string; dir: string; profile?: Profile }
export type Found = { name: string; dir: string; listed?: string }

export type Answers = { round?: string[]; repos?: string[]; ticket?: string[] }
