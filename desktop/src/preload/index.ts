import { contextBridge, ipcRenderer } from 'electron'

type BackendState = { status: 'starting' } | { status: 'ready' } | { status: 'failed'; error: string }

contextBridge.exposeInMainWorld('refine', {
  api: (method: string, path: string, body?: unknown) =>
    ipcRenderer.invoke('api', { method, path, body }),
  backendState: (): Promise<BackendState> => ipcRenderer.invoke('backend-state'),
  restartBackend: (): Promise<BackendState> => ipcRenderer.invoke('restart-backend'),
  onBackendState: (listener: (state: BackendState) => void) => {
    const handler = (_event: unknown, state: BackendState) => listener(state)
    ipcRenderer.on('backend-state', handler)
    return () => {
      ipcRenderer.removeListener('backend-state', handler)
    }
  },
  pickFolder: (start?: string): Promise<string | null> => ipcRenderer.invoke('pick-folder', start),
  openExternal: (url: string) => ipcRenderer.invoke('open-external', url)
})
