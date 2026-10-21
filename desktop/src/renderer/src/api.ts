import type { Found, Info, Issue, Job, JobKind, Repo, RepoStatus, Saved, SavedSet, Written } from './types'

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number
  ) {
    super(message)
  }
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await window.refine.api(method, path, body)
  if (!res.ok) {
    const error = (res.data as { error?: string } | null)?.error ?? `request failed (${res.status})`
    throw new ApiError(error, res.status)
  }
  return res.data as T
}

const key = (k: string) => encodeURIComponent(k)

export const api = {
  info: () => call<Info>('GET', '/api/info'),
  event: (name: string, key: string, props: Record<string, unknown>) =>
    call<null>('POST', '/api/events', { name, key, props }),
  tickets: () => call<{ user: string; issues: Issue[] }>('GET', '/api/tickets'),
  lookup: (q: string) => call<Issue>('GET', `/api/lookup?q=${encodeURIComponent(q)}`),
  hide: (k: string) => call<null>('PUT', `/api/hidden/${key(k)}`),
  unhide: (k: string) => call<null>('DELETE', `/api/hidden/${key(k)}`),
  saved: (k: string) => call<SavedSet>('GET', `/api/tickets/${key(k)}/saved`),
  answers: (k: string, draftID: number, answers: Record<string, string>) =>
    call<Saved>('PUT', `/api/tickets/${key(k)}/answers`, { draft_id: draftID, answers }),
  reveal: (k: string) => call<{ path: string }>('POST', `/api/tickets/${key(k)}/reveal`),
  write: (k: string, draftID: number) =>
    call<Written>('POST', `/api/tickets/${key(k)}/write`, { draft_id: draftID }),
  move: (k: string) => call<null>('POST', `/api/tickets/${key(k)}/move`),
  check: () => call<RepoStatus[]>('POST', '/api/sync/check'),
  pull: (stale: RepoStatus[]) =>
    call<{ pulled: RepoStatus[]; error?: string }>('POST', '/api/sync/pull', stale),
  repos: () => call<Repo[]>('GET', '/api/repos'),
  addRepo: (name: string, dir: string) => call<Repo[]>('POST', '/api/repos', { name, dir }),
  removeRepo: (name: string) => call<Repo[]>('DELETE', `/api/repos/${encodeURIComponent(name)}`),
  scanRepos: (dir: string) => call<Found[]>('GET', `/api/repos/scan?dir=${encodeURIComponent(dir)}`),
  folderStart: () => call<{ start: string }>('GET', '/api/folders'),
  jobs: () => call<Job[]>('GET', '/api/jobs'),
  startJob: (
    kind: JobKind,
    k: string,
    opts: { approach?: string; scope?: Record<string, string>; resume?: boolean } = {}
  ) =>
    call<Job>('POST', '/api/jobs', { kind, key: k, ...opts }),
  job: (id: string) => call<Job>('GET', `/api/jobs/${id}`),
  cancelJob: (id: string) => call<null>('DELETE', `/api/jobs/${id}`)
}
