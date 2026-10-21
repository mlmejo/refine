import { useRefine } from '../store'

// SyncDialog asks what to do when Claude would read code behind origin.
export function SyncDialog() {
  const refine = useRefine()
  const ask = refine.syncAsk!
  return (
    <div className="overlay" onClick={refine.syncCancel}>
      <div className="dialog" onClick={(e) => e.stopPropagation()}>
        <h2>Claude would read code that is behind origin</h2>
        <ul className="bullets">
          {ask.stale.map((s) => (
            <li key={s.name}>
              <span className="marker">•</span>
              <span>
                <span className="key">{s.name}</span>: {s.branch} is {s.behind === 1 ? '1 commit' : `${s.behind} commits`}{' '}
                behind origin/{s.branch}
              </span>
            </li>
          ))}
        </ul>
        <p className="quiet small">Pull fast-forwards each one; it never merges or touches local changes.</p>
        {ask.error && <p className="error-text">could not pull: {ask.error}</p>}
        <div className="actions">
          <button className="primary" disabled={ask.pulling} onClick={() => void refine.syncPull()}>
            {ask.pulling ? 'Pulling…' : 'Pull now'}
          </button>
          <button disabled={ask.pulling} onClick={() => void refine.syncContinue()}>
            Continue on stale code
          </button>
          <button disabled={ask.pulling} onClick={refine.syncCancel}>
            Cancel
          </button>
        </div>
      </div>
    </div>
  )
}
