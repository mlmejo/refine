import { join } from 'node:path'
import { app, BrowserWindow, dialog, ipcMain, shell } from 'electron'
import { startBackend, stopBackend, useLoginPath, type Backend, type BackendState } from './backend'

let backend: Backend | undefined
let state: BackendState = { status: 'starting' }
let win: BrowserWindow | undefined

function setState(next: BackendState): void {
  state = next
  win?.webContents.send('backend-state', state)
}

async function boot(): Promise<void> {
  setState({ status: 'starting' })
  try {
    backend = await startBackend((error) => {
      backend = undefined
      setState({ status: 'failed', error })
    })
    setState({ status: 'ready' })
  } catch (err) {
    setState({ status: 'failed', error: (err as Error).message })
  }
}

function createWindow(): void {
  win = new BrowserWindow({
    width: 1280,
    height: 860,
    minWidth: 900,
    minHeight: 600,
    title: 'refine',
    titleBarStyle: 'hiddenInset',
    backgroundColor: '#16161a',
    webPreferences: {
      preload: join(__dirname, '../preload/index.js'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true
    }
  })
  // Links open in the browser, never in the app.
  win.webContents.setWindowOpenHandler(({ url }) => {
    if (url.startsWith('https://')) void shell.openExternal(url)
    return { action: 'deny' }
  })
  win.webContents.on('will-navigate', (event) => event.preventDefault())
  win.on('closed', () => (win = undefined))

  if (process.env.ELECTRON_RENDERER_URL) {
    void win.loadURL(process.env.ELECTRON_RENDERER_URL)
  } else {
    void win.loadFile(join(__dirname, '../renderer/index.html'))
  }
}

type ApiRequest = { method: string; path: string; body?: unknown }
type ApiResponse = { ok: boolean; status: number; data: unknown }

// The renderer reaches the backend only through here, so the token stays in
// the main process.
ipcMain.handle('api', async (_event, req: ApiRequest): Promise<ApiResponse> => {
  if (!backend) return { ok: false, status: 0, data: { error: 'refine is not running' } }
  if (!req.path.startsWith('/api/')) return { ok: false, status: 0, data: { error: 'bad path' } }
  try {
    const res = await fetch(`http://127.0.0.1:${backend.port}${req.path}`, {
      method: req.method,
      headers: {
        Authorization: `Bearer ${backend.token}`,
        ...(req.body === undefined ? {} : { 'Content-Type': 'application/json' })
      },
      body: req.body === undefined ? undefined : JSON.stringify(req.body)
    })
    const text = await res.text()
    return { ok: res.ok, status: res.status, data: text ? JSON.parse(text) : null }
  } catch (err) {
    return { ok: false, status: 0, data: { error: (err as Error).message } }
  }
})

ipcMain.handle('backend-state', () => state)
ipcMain.handle('restart-backend', async () => {
  stopBackend(backend)
  backend = undefined
  await boot()
  return state
})

ipcMain.handle('pick-folder', async (_event, start: string | undefined, purpose: 'repos' | 'claude' | undefined) => {
  // A Claude config directory is a dot folder, such as ~/.claude-team, so
  // the picker shows hidden folders for it.
  const claude = purpose === 'claude'
  const result = await dialog.showOpenDialog(win!, {
    title: claude ? 'Pick the Claude Code config folder refine should use' : 'Pick a folder; refine finds the git repos in it',
    buttonLabel: claude ? 'Use' : 'Scan',
    defaultPath: start,
    properties: claude ? ['openDirectory', 'showHiddenFiles'] : ['openDirectory']
  })
  return result.canceled ? null : result.filePaths[0]
})

ipcMain.handle('open-external', (_event, url: string) => {
  if (url.startsWith('https://')) void shell.openExternal(url)
})

app.whenReady().then(() => {
  useLoginPath()
  createWindow()
  void boot()
  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow()
  })
})

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit()
})

app.on('before-quit', () => stopBackend(backend))
