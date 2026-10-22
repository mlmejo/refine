import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { api } from './api'
import type { Info, Issue, Job, JobKind, RepoStatus, Saved, SavedSet, TicketContext } from './types'

export type Tab = 'ticket' | 'triage' | 'brief' | 'approach' | 'draft'

// TicketState is what the app knows about one ticket beyond the list.
export type TicketState = {
  saved?: SavedSet
  tab: Tab
  job?: Job
  // error is the last run's or action's failure, shown on the ticket.
  error?: string
  // notes are from checking the repos, shown while Claude works.
  notes: string[]
  // approach is the text being written, kept across tab switches, and scope
  // the answers to the triage's scope questions, keyed by question.
  approach?: string
  scope?: Record<string, string>
  // context is the ticket as Jira has it, read once when first shown.
  context?: TicketContext
  contextError?: string
}

// SyncAsk is the stale-repos question before a run reads the code.
export type SyncAsk = {
  key: string
  stale: RepoStatus[]
  notes: string[]
  error?: string
  pulling?: boolean
  run: (notes: string[]) => Promise<void>
}

type Removed = { issue: Issue; index: number; pinned: boolean }

export type Flash = { text: string; error?: boolean }

function commits(n: number): string {
  return n === 1 ? '1 commit' : `${n} commits`
}

function staleNote(s: RepoStatus): string {
  return `${s.name}: reading ${s.branch} ${commits(s.behind)} behind origin`
}

function useRefineState() {
  const [info, setInfo] = useState<Info>()
  const [user, setUser] = useState('')
  const [listed, setListed] = useState<Issue[]>([])
  const [pinned, setPinned] = useState<Issue[]>([])
  const [loading, setLoading] = useState(false)
  const [listError, setListError] = useState<string>()
  const [selected, setSelected] = useState<string>()
  const [tickets, setTickets] = useState<Record<string, TicketState>>({})
  const [removed, setRemoved] = useState<Removed[]>([])
  const [syncAsk, setSyncAsk] = useState<SyncAsk>()
  const [checking, setChecking] = useState<string>()
  const [flash, setFlash] = useState<Flash>()
  // declined remembers, per repo, the origin commit the developer chose to
  // read stale code at, so they are not asked again until origin moves.
  const declined = useRef<Record<string, string>>({})

  // Tickets opened by key come first, unless the list already has them.
  const issues = useMemo(() => {
    const keys = new Set(listed.map((i) => i.key))
    return [...pinned.filter((p) => !keys.has(p.key)), ...listed]
  }, [listed, pinned])

  // track records a usage event when the tracking flag is on. It never
  // throws: tracking must not get in the way of refining.
  const trackingOn = info?.tracking ?? false
  const track = useCallback(
    (name: string, key = '', props: Record<string, unknown> = {}) => {
      if (trackingOn) void api.event(name, key, props).catch(() => undefined)
    },
    [trackingOn]
  )

  // Time away from the window is not time spent on a screen.
  useEffect(() => {
    if (!trackingOn) return
    const onVisible = () => track(document.hidden ? 'blur' : 'focus')
    const onBlur = () => track('blur')
    const onFocus = () => track('focus')
    document.addEventListener('visibilitychange', onVisible)
    window.addEventListener('blur', onBlur)
    window.addEventListener('focus', onFocus)
    track('open')
    return () => {
      document.removeEventListener('visibilitychange', onVisible)
      window.removeEventListener('blur', onBlur)
      window.removeEventListener('focus', onFocus)
    }
  }, [trackingOn, track])

  const ticket = useCallback(
    (key: string): TicketState => tickets[key] ?? { tab: 'ticket', notes: [] },
    [tickets]
  )

  const patch = useCallback((key: string, change: Partial<TicketState> | ((t: TicketState) => Partial<TicketState>)) => {
    setTickets((all) => {
      const current = all[key] ?? { tab: 'ticket', notes: [] }
      const next = typeof change === 'function' ? change(current) : change
      return { ...all, [key]: { ...current, ...next } }
    })
  }, [])

  const refresh = useCallback(async () => {
    setLoading(true)
    setListError(undefined)
    try {
      const page = await api.tickets()
      setUser(page.user)
      setListed(page.issues)
    } catch (err) {
      setListError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  const loadSaved = useCallback(
    async (key: string) => {
      try {
        const saved = await api.saved(key)
        patch(key, { saved })
      } catch (err) {
        patch(key, { error: (err as Error).message })
      }
    },
    [patch]
  )

  const select = useCallback(
    (key: string) => {
      setSelected(key)
      track('view', key, { tab: tickets[key]?.tab ?? 'ticket' })
      if (!tickets[key]?.saved) void loadSaved(key)
    },
    [tickets, loadSaved, track]
  )

  const loadContext = useCallback(
    async (key: string) => {
      try {
        patch(key, { context: await api.context(key), contextError: undefined })
      } catch (err) {
        patch(key, { contextError: (err as Error).message })
      }
    },
    [patch]
  )

  const setTab = useCallback(
    (key: string, tab: Tab) => {
      track('view', key, { tab })
      patch(key, { tab, error: undefined })
    },
    [patch, track]
  )

  // Boot: info, tickets, and any runs still going from before a reload.
  useEffect(() => {
    void (async () => {
      try {
        setInfo(await api.info())
      } catch (err) {
        setListError((err as Error).message)
      }
      void refresh()
      try {
        for (const job of await api.jobs()) patch(job.key, { job })
      } catch {
        // No runs to pick up.
      }
    })()
  }, [refresh, patch])

  const replaceIssue = useCallback((issue: Issue) => {
    const swap = (list: Issue[]) => list.map((i) => (i.key === issue.key ? issue : i))
    setListed(swap)
    setPinned(swap)
  }, [])

  // dropIssue takes a ticket that left the sprint off the list, unhidden.
  const dropIssue = useCallback(
    (key: string) => {
      setListed((l) => l.filter((i) => i.key !== key))
      setPinned((p) => p.filter((i) => i.key !== key))
      if (selected === key) setSelected(undefined)
    },
    [selected]
  )

  const openByKey = useCallback(
    async (q: string): Promise<string | undefined> => {
      const found = issues.find((i) => i.key.toLowerCase() === q.trim().toLowerCase())
      if (found) {
        select(found.key)
        return
      }
      try {
        const issue = await api.lookup(q)
        if (!issues.some((i) => i.key === issue.key)) setPinned((p) => [issue, ...p])
        select(issue.key)
      } catch (err) {
        return (err as Error).message
      }
      return
    },
    [issues, select]
  )

  // remove takes a ticket off the list: unpins it, or hides it for good.
  const remove = useCallback(
    async (key: string) => {
      const index = issues.findIndex((i) => i.key === key)
      if (index < 0) return
      const issue = issues[index]
      const isPinned = pinned.some((p) => p.key === key)
      if (isPinned) {
        setPinned((p) => p.filter((i) => i.key !== key))
      } else {
        try {
          await api.hide(key)
        } catch (err) {
          setFlash({ text: (err as Error).message, error: true })
          return
        }
        setListed((l) => l.filter((i) => i.key !== key))
      }
      setRemoved((r) => [...r, { issue, index, pinned: isPinned }])
      if (selected === key) setSelected(undefined)
    },
    [issues, pinned, selected]
  )

  const undo = useCallback(async () => {
    const gone = removed[removed.length - 1]
    if (!gone) return
    setRemoved((r) => r.slice(0, -1))
    if (gone.pinned) {
      setPinned((p) => [gone.issue, ...p])
    } else {
      try {
        await api.unhide(gone.issue.key)
      } catch (err) {
        setFlash({ text: (err as Error).message, error: true })
        return
      }
      setListed((l) => {
        if (l.some((i) => i.key === gone.issue.key)) return l
        const next = [...l]
        next.splice(Math.min(gone.index, next.length), 0, gone.issue)
        return next
      })
    }
    select(gone.issue.key)
  }, [removed, select])

  const move = useCallback(
    async (key: string) => {
      try {
        await api.move(key)
        dropIssue(key)
        setFlash({ text: `moved ${key} to ${info?.ready_sprint}` })
      } catch (err) {
        setFlash({ text: (err as Error).message, error: true })
      }
    },
    [dropIssue, info]
  )

  const startJob = useCallback(
    async (
      kind: JobKind,
      key: string,
      opts: { approach?: string; scope?: Record<string, string>; resume?: boolean } = {},
      notes: string[] = []
    ) => {
      patch(key, { error: undefined, notes })
      try {
        const job = await api.startJob(kind, key, opts)
        patch(key, { job })
      } catch (err) {
        patch(key, { error: (err as Error).message })
      }
    },
    [patch]
  )

  // gate checks the repos are up to date before a run reads the code, asking
  // to pull any that are behind origin.
  const gate = useCallback(
    async (key: string, run: (notes: string[]) => Promise<void>) => {
      if (!info?.sync) return run([])
      setChecking(key)
      let statuses: RepoStatus[]
      try {
        statuses = await api.check()
      } catch (err) {
        patch(key, { error: `could not check the repos: ${(err as Error).message}` })
        return
      } finally {
        setChecking(undefined)
      }
      const notes: string[] = []
      const stale: RepoStatus[] = []
      for (const s of statuses) {
        if (s.skip) notes.push(`${s.name}: ${s.skip}`)
        else if (s.behind > 0 && declined.current[s.name] !== s.origin) stale.push(s)
        else if (s.behind > 0) notes.push(staleNote(s))
      }
      if (stale.length === 0) return run(notes)
      setSyncAsk({ key, stale, notes, run })
    },
    [info, patch]
  )

  const syncContinue = useCallback(async () => {
    if (!syncAsk) return
    const notes = [...syncAsk.notes]
    for (const s of syncAsk.stale) {
      declined.current[s.name] = s.origin
      notes.push(staleNote(s))
    }
    setSyncAsk(undefined)
    await syncAsk.run(notes)
  }, [syncAsk])

  const syncPull = useCallback(async () => {
    if (!syncAsk) return
    setSyncAsk({ ...syncAsk, pulling: true, error: undefined })
    try {
      const out = await api.pull(syncAsk.stale)
      const notes = [...syncAsk.notes, ...out.pulled.map((s) => `${s.name}: pulled ${s.branch}, ${commits(s.behind)}`)]
      if (!out.error) {
        setSyncAsk(undefined)
        await syncAsk.run(notes)
        return
      }
      const done = new Set(out.pulled.map((s) => s.name))
      setSyncAsk({ ...syncAsk, notes, stale: syncAsk.stale.filter((s) => !done.has(s.name)), error: out.error, pulling: false })
    } catch (err) {
      setSyncAsk({ ...syncAsk, error: (err as Error).message, pulling: false })
    }
  }, [syncAsk])

  const triage = useCallback((key: string) => startJob('triage', key), [startJob])
  const brief = useCallback((key: string) => gate(key, (notes) => startJob('brief', key, {}, notes)), [gate, startJob])
  const ask = useCallback(
    (key: string, approach: string, scope: Record<string, string>) =>
      gate(key, (notes) => startJob('draft', key, { approach, scope }, notes)),
    [gate, startJob]
  )
  const nextRound = useCallback((key: string) => startJob('draft', key, { resume: true }), [startJob])
  // stamp reads code only to confirm paths, so it skips the repo check.
  const stamp = useCallback((key: string) => startJob('stamp', key), [startJob])

  const cancel = useCallback(
    async (key: string) => {
      const job = tickets[key]?.job
      if (!job) return
      track('cancel_run', key, { kind: job.kind, secs: (Date.now() - new Date(job.started).getTime()) / 1000 })
      await api.cancelJob(job.id).catch(() => undefined)
      patch(key, { job: undefined, error: `${job.kind === 'draft' ? 'questions' : job.kind} cancelled` })
    },
    [tickets, patch, track]
  )

  // Poll the running jobs once a second.
  const running = useMemo(
    () => Object.values(tickets).flatMap((t) => (t.job && !t.job.done ? [t.job] : [])),
    [tickets]
  )
  // The timer restarts only when the set of runs changes, not on every edit.
  const runningRef = useRef(running)
  runningRef.current = running
  const runningIDs = running.map((j) => j.id).join(',')
  useEffect(() => {
    if (runningIDs === '') return
    const timer = setInterval(() => {
      for (const job of runningRef.current) {
        api
          .job(job.id)
          .then((next) => {
            if (!next.done) return
            patch(next.key, (t) => {
              if (t.job?.id !== next.id) return {}
              if (next.error) return { job: undefined, error: next.error }
              const saved: SavedSet = t.saved ?? { draft: null, brief: null, triage: null }
              const tab: Tab = next.triage ? 'triage' : next.brief ? 'brief' : 'draft'
              track('view', next.key, { tab, after_run: next.kind })
              if (next.triage) return { job: undefined, tab, saved: { ...saved, triage: next.triage } }
              if (next.brief) return { job: undefined, tab, saved: { ...saved, brief: next.brief } }
              return { job: undefined, tab, saved: { ...saved, draft: next.draft ?? saved.draft } }
            })
            // A fresh draft triages the ticket first when it had none.
            if (next.kind === 'draft' && !next.error) void loadSaved(next.key)
          })
          .catch((err) => {
            patch(job.key, (t) => (t.job?.id === job.id ? { job: undefined, error: (err as Error).message } : {}))
          })
      }
    }, 1000)
    return () => clearInterval(timer)
  }, [runningIDs, patch, loadSaved, track])

  const setDraft = useCallback(
    (key: string, draft: Saved) => patch(key, (t) => ({ saved: { ...(t.saved ?? { brief: null, triage: null }), draft } })),
    [patch]
  )

  return {
    track,
    info,
    user,
    issues,
    pinned,
    loading,
    listError,
    selected,
    removed,
    syncAsk,
    checking,
    flash,
    setFlash,
    ticket,
    patch,
    refresh,
    select,
    setTab,
    openByKey,
    remove,
    undo,
    move,
    replaceIssue,
    dropIssue,
    loadSaved,
    loadContext,
    triage,
    brief,
    ask,
    nextRound,
    stamp,
    cancel,
    setDraft,
    syncContinue,
    syncPull,
    syncCancel: () => setSyncAsk(undefined)
  }
}

export type Refine = ReturnType<typeof useRefineState>

const Context = createContext<Refine | null>(null)

export function RefineProvider({ children }: { children: ReactNode }) {
  const value = useRefineState()
  return <Context.Provider value={value}>{children}</Context.Provider>
}

export function useRefine(): Refine {
  const value = useContext(Context)
  if (!value) throw new Error('useRefine outside RefineProvider')
  return value
}
