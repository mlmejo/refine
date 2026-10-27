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
  | { status: 'setup' }
  | { status: 'ready' }
  | { status: 'failed'; error: string }

// Account is the Jira account refine setup writes to the config.
export type Account = { site: string; email: string; token: string }

// exitNotSetUp is refine serve's exit code when the config lacks the Jira
// account.
const exitNotSetUp = 3

// NotSetUp is startBackend's error when refine needs the Jira account first.
export class NotSetUp extends Error {}

// binary finds refine: REFINE_BIN, the copy bundled with the app, or the one
// `pnpm go:build` puts in the repo's bin/.
function binary(): string {
  if (process.env.REFINE_BIN) return process.env.REFINE_BIN
  if (app.isPackaged) return join(process.resourcesPath, 'refine')
  return resolve(app.getAppPath(), '..', 'bin', 'refine')
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

export function startBackend(onExit: (error: string) => void): Promise<Backend> {
  const bin = binary()
  if (!existsSync(bin)) {
    return Promise.reject(new Error(`refine binary not found at ${bin}; run pnpm go:build`))
  }
  const child = spawn(bin, ['serve'], { stdio: ['pipe', 'pipe', 'pipe'] })
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
      if (!ready) reject(code === exitNotSetUp ? new NotSetUp(error) : new Error(error))
      else onExit(error)
    })
  })
}

// saveAccount runs refine setup, which checks the account with Jira and
// writes it to the config. It returns the account's display name. The token
// goes over stdin, never the command line.
export function saveAccount(account: Account): Promise<string> {
  const bin = binary()
  const child = spawn(bin, ['setup'], { stdio: ['pipe', 'pipe', 'pipe'] })
  let stdout = ''
  let stderr = ''
  child.stdout.on('data', (chunk) => (stdout += chunk.toString()))
  child.stderr.on('data', (chunk) => (stderr += chunk.toString()))
  child.stdin.end(JSON.stringify(account))

  return new Promise((resolvePromise, reject) => {
    child.once('error', (err) => reject(err))
    child.once('exit', (code) => {
      if (code !== 0) {
        reject(new Error(stderr.trim().replace(/^refine: /, '') || `refine setup exited with code ${code}`))
        return
      }
      try {
        resolvePromise((JSON.parse(stdout) as { name: string }).name)
      } catch {
        reject(new Error(`refine setup said: ${stdout}`))
      }
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
