import type { Answers, Found, Info, Job, JobKind, Page, QA, Repo, Result, View, Writeup } from './types'

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
const ticket = (k: string) => `/api/tickets/${key(k)}`

export const api = {
  info: () => call<Info>('GET', '/api/info'),
  tickets: () => call<Page>('GET', '/api/tickets'),
  lookup: (q: string) => call<{ key: string }>('GET', `/api/lookup?q=${encodeURIComponent(q)}`),
  hide: (k: string) => call<null>('PUT', `/api/hidden/${key(k)}`),

  open: (k: string) => call<View>('GET', ticket(k)),
  reset: (k: string) => call<null>('DELETE', ticket(k)),
  decide: (k: string, blocker: string, decision: 'accepted' | 'rejected') =>
    call<View>('PUT', `${ticket(k)}/decisions`, { blocker, decision }),
  proceed: (k: string) => call<View>('POST', `${ticket(k)}/continue`),
  setRepos: (k: string, repos: string[]) => call<View>('PUT', `${ticket(k)}/repos`, { repos }),
  suggestRepos: (k: string) => call<View>('POST', `${ticket(k)}/suggestion`),
  sync: (k: string) => call<View>('POST', `${ticket(k)}/sync`),
  chooseRepos: (k: string) => call<View>('DELETE', `${ticket(k)}/sync`),
  restartQuestions: (k: string) => call<View>('DELETE', `${ticket(k)}/rounds`),
  answer: (k: string, answers: Answers) => call<View>('PUT', `${ticket(k)}/answers`, answers),
  edit: (k: string, result: Result) => call<View>('PUT', `${ticket(k)}/result`, result),
  stamp: (k: string) => call<View>('POST', `${ticket(k)}/stamp`),

  job: () => call<Job | null>('GET', '/api/job'),
  startJob: (kind: JobKind, k: string, explanation?: string) =>
    call<Job>('POST', '/api/jobs', { kind, key: k, explanation }),
  cancelJob: (id: string) => call<null>('DELETE', `/api/jobs/${id}`),

  repos: () => call<Repo[]>('GET', '/api/repos'),
  addRepo: (name: string, dir: string) => call<Repo[]>('POST', '/api/repos', { name, dir }),
  removeRepo: (name: string) => call<Repo[]>('DELETE', `/api/repos/${encodeURIComponent(name)}`),
  scanRepos: (dir: string) => call<Found[]>('GET', `/api/repos/scan?dir=${encodeURIComponent(dir)}`),
  folderStart: () => call<{ start: string }>('GET', '/api/folders'),
  setProfile: (repo: string, answers: QA[]) =>
    call<Repo[]>('PUT', `/api/profiles/${encodeURIComponent(repo)}`, { answers }),

  writeup: () => call<Writeup>('GET', '/api/writeup'),
  uploadWriteup: (text: string) => call<Writeup>('PUT', '/api/writeup', { text }),
  resetWriteup: () => call<Writeup>('DELETE', '/api/writeup')
}
