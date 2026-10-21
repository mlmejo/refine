import { spawn, execFileSync, type ChildProcess } from 'node:child_process'
import { existsSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { createInterface } from 'node:readline'
import { app } from 'electron'

// Backend is the Go refine binary run as `refine serve`. It prints its port
// and token as its first line, and exits when its stdin closes.
export type Backend = {
  port: number
  token: string
  process: ChildProcess
}

export type BackendState =
  | { status: 'starting' }
  | { status: 'ready' }
  | { status: 'failed'; error: string }

// binary finds refine: REFINE_BIN, the copy bundled with the app, or the one
// `pnpm go:build` puts in the repo's bin/.
function binary(): string {
  if (process.env.REFINE_BIN) return process.env.REFINE_BIN
  if (app.isPackaged) return join(process.resourcesPath, 'refine')
  return resolve(app.getAppPath(), '..', 'bin', 'refine')
}

// projectDir is where refine runs, so its config reads the project's .env:
// REFINE_DIR, else the repo this app sits in.
function projectDir(): string {
  return process.env.REFINE_DIR || resolve(app.getAppPath(), '..')
}

// loginPath reads PATH from the login shell. An app opened from the Finder
// gets a bare PATH, and refine needs claude and git from the user's.
export function useLoginPath(): void {
  if (process.platform === 'win32') return
  const shell = process.env.SHELL || '/bin/zsh'
  try {
    const out = execFileSync(shell, ['-ilc', 'printf "__PATH__%s" "$PATH"'], {
      encoding: 'utf8',
      timeout: 5000
    })
    const path = out.split('__PATH__').pop()?.trim()
    if (path) process.env.PATH = path
  } catch {
    // Keep the PATH we have; a missing claude shows up as a run error.
  }
}

// refArg passes --ref through, from REFINE_REF or the app's own arguments.
function refArgs(): string[] {
  const ref =
    process.env.REFINE_REF ||
    process.argv.find((a) => a.startsWith('--ref='))?.slice('--ref='.length)
  return ref ? ['--ref', ref] : []
}

export function startBackend(onExit: (error: string) => void): Promise<Backend> {
  const bin = binary()
  if (!existsSync(bin)) {
    return Promise.reject(new Error(`refine binary not found at ${bin}; run pnpm go:build`))
  }
  const child = spawn(bin, [...refArgs(), 'serve'], { cwd: projectDir(), stdio: ['pipe', 'pipe', 'pipe'] })
  let stderr = ''
  child.stderr.on('data', (chunk) => {
    stderr = (stderr + chunk.toString()).slice(-4000)
  })

  return new Promise((resolvePromise, reject) => {
    let ready = false
    const lines = createInterface({ input: child.stdout })
    lines.once('line', (line) => {
      try {
        const hello = JSON.parse(line) as { port: number; token: string }
        ready = true
        resolvePromise({ port: hello.port, token: hello.token, process: child })
      } catch {
        reject(new Error(`refine serve said: ${line}`))
      }
    })
    child.once('error', (err) => reject(err))
    child.once('exit', (code) => {
      const error = stderr.trim() || `refine serve exited with code ${code}`
      if (!ready) reject(new Error(error))
      else onExit(error)
    })
  })
}

// stopBackend closes refine's stdin, which shuts it down and stops its runs.
export function stopBackend(backend: Backend | undefined): void {
  if (!backend || backend.process.exitCode !== null) return
  backend.process.stdin?.end()
  setTimeout(() => {
    if (backend.process.exitCode === null) backend.process.kill()
  }, 3000).unref()
}
