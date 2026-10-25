import { useEffect, useState } from 'react'
import { useRefine } from '../store'
import type { Job } from '../types'

const titles: Record<Job['kind'], string> = {
  search: 'Claude is looking for related tickets',
  ask: 'Claude is reading the code for the next questions',
  deploy: 'Claude is reading how the repos deploy',
  generate: 'Claude is writing up the ticket'
}

const hints: Record<Job['kind'], string> = {
  search: 'No code, so usually well under a minute.',
  ask: 'Usually about 2 minutes.',
  deploy: 'Usually a minute or two.',
  generate: 'Usually about 2 minutes.'
}

function clock(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000))
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}

// Working shows a run's progress: what Claude reads, as it reads it.
export function Working({ job }: { job: Job }) {
  const refine = useRefine()
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [])
  const log = job.log ?? []
  return (
    <div className="scroll">
      <section className="working" aria-live="polite">
        <div className="row" style={{ gap: 10 }}>
          <span className="spinner" />
          <h2 style={{ margin: 0, fontSize: 14 }}>{titles[job.kind]}</h2>
          <span className="muted tabular" style={{ marginLeft: 'auto' }}>
            {clock(now - new Date(job.started).getTime())}
          </span>
        </div>
        <p className="muted" style={{ margin: 0 }}>
          {hints[job.kind]} If you switch to another app, Refine notifies you when it is done.
        </p>
        {log.length > 0 && (
          <ol className="log">
            {log.slice(-200).map((a, i) => (
              <li key={i}>
                <span className="verb">{a.verb}</span>
                <span className="what">{a.what}</span>
                <span className="small muted tabular">{clock(new Date(a.at).getTime() - new Date(job.started).getTime())}</span>
              </li>
            ))}
          </ol>
        )}
        <div className="row">
          <button className="btn" style={{ marginLeft: 'auto' }} onClick={() => void refine.cancel()}>
            Cancel run
          </button>
        </div>
      </section>
    </div>
  )
}
