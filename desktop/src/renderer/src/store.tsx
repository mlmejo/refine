import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { api } from './api'
import type { Info, Job, JobKind, Page, View } from './types'

export type Screen = { kind: 'tickets' } | { kind: 'ticket'; key: string } | { kind: 'repos' } | { kind: 'claude' }

export type Toast = { text: string; error?: boolean }

const jobNames: Record<JobKind, string> = {
  search: 'Related tickets are ready to review',
  ask: 'Questions are ready',
  deploy: 'Deployment questions are ready',
  generate: 'The results are ready to review',
  rewrite: 'Acceptance Criteria and Technical Implementation are rewritten',
  merge: 'The ticket is merged into one'
}

function useRefineState() {
  const [info, setInfo] = useState<Info>()
  const [page, setPage] = useState<Page>()
  const [pageError, setPageError] = useState<string>()
  const [loading, setLoading] = useState(false)
  const [screen, setScreen] = useState<Screen>({ kind: 'tickets' })
  const [views, setViews] = useState<Record<string, View>>({})
  const [errors, setErrors] = useState<Record<string, string | undefined>>({})
  const [job, setJob] = useState<Job | null>(null)
  const [pending, setPending] = useState<string>()
  const [palette, setPalette] = useState(false)
  const [toast, setToast] = useState<Toast>()
  const toastTimer = useRef<number>(undefined)

  const flash = useCallback((t: Toast) => {
    setToast(t)
    window.clearTimeout(toastTimer.current)
    toastTimer.current = window.setTimeout(() => setToast(undefined), 5000)
  }, [])

  const setError = useCallback((key: string, error?: string) => setErrors((all) => ({ ...all, [key]: error })), [])

  // keep stores a view. A view after a change has no ticket, since the
  // backend does not read Jira again; the one already shown is kept.
  const keep = useCallback((view: View) => {
    const key = view.refinement.key
    setViews((all) => {
      const before = all[key]
      const ticket = view.ticket.key ? view.ticket : (before?.ticket ?? view.ticket)
      return { ...all, [key]: { ...view, ticket } }
    })
    return view
  }, [])

  const refresh = useCallback(async () => {
    setLoading(true)
    setPageError(undefined)
    try {
      setPage(await api.tickets())
    } catch (err) {
      setPageError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  const load = useCallback(
    async (key: string) => {
      setError(key)
      try {
        return keep(await api.open(key))
      } catch (err) {
        setError(key, (err as Error).message)
      }
    },
    [keep, setError]
  )

  const open = useCallback(
    (key: string) => {
      setScreen({ kind: 'ticket', key })
      void load(key)
    },
    [load]
  )

  // act runs a change on a ticket that replies with its view.
  const act = useCallback(
    async (key: string, name: string, change: () => Promise<View>) => {
      setPending(name)
      setError(key)
      try {
        return keep(await change())
      } catch (err) {
        setError(key, (err as Error).message)
      } finally {
        setPending(undefined)
      }
    },
    [keep, setError]
  )

  const run = useCallback(
    async (kind: JobKind, key: string, explanation?: string) => {
      setError(key)
      try {
        setJob(await api.startJob(kind, key, explanation))
      } catch (err) {
        setError(key, (err as Error).message)
      }
    },
    [setError]
  )

  const cancel = useCallback(async () => {
    if (!job) return
    await api.cancelJob(job.id).catch(() => undefined)
    setJob(null)
  }, [job])

  // next starts the step that follows a finished run on its own: deployment
  // questions after the last round, the write-up when nothing needs asking.
  const next = useCallback(
    (finished: JobKind, view: View) => {
      const r = view.refinement
      if (finished === 'ask' && r.step === 'deploy' && !r.deploy) void run('deploy', r.key)
      if (finished === 'deploy' && r.step === 'deploy' && r.deploy) {
        const empty = !(r.deploy.repos ?? []).length && !(r.deploy.ticket ?? []).length
        if (empty) void run('generate', r.key)
      }
    },
    [run]
  )

  // Poll the running job; when it finishes, reload its ticket.
  useEffect(() => {
    if (!job || job.done) return
    const timer = window.setInterval(async () => {
      const current = await api.job().catch(() => null)
      if (!current || current.id !== job.id) {
        setJob(null)
        return
      }
      setJob(current)
      if (!current.done) return
      if (current.error) {
        setError(current.key, current.error)
        if (!document.hasFocus()) new Notification(current.key, { body: 'Claude stopped: ' + current.error })
      } else if (!document.hasFocus()) {
        new Notification(current.key, { body: jobNames[current.kind] })
      }
      const view = await load(current.key)
      void refresh()
      setJob(null)
      if (view && !current.error) next(current.kind, view)
    }, 1000)
    return () => window.clearInterval(timer)
  }, [job, load, refresh, next, setError])

  useEffect(() => {
    void api.info().then(setInfo, () => undefined)
    void refresh()
    // A run started before the app was reloaded is picked up again.
    void api.job().then((j) => j && !j.done && setJob(j), () => undefined)
  }, [refresh])

  return {
    info,
    page,
    pageError,
    loading,
    refresh,
    screen,
    setScreen,
    views,
    errors,
    setError,
    load,
    open,
    act,
    keep,
    run,
    cancel,
    job,
    pending,
    palette,
    setPalette,
    toast,
    flash
  }
}

export type RefineState = ReturnType<typeof useRefineState>

const RefineContext = createContext<RefineState | null>(null)

export function RefineProvider({ children }: { children: ReactNode }) {
  const state = useRefineState()
  return <RefineContext.Provider value={state}>{children}</RefineContext.Provider>
}

export function useRefine(): RefineState {
  const state = useContext(RefineContext)
  if (!state) throw new Error('useRefine outside RefineProvider')
  return state
}
