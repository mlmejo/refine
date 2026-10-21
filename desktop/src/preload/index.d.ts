export type BackendState =
  | { status: 'starting' }
  | { status: 'ready' }
  | { status: 'failed'; error: string }

export type ApiResponse = { ok: boolean; status: number; data: unknown }

declare global {
  interface Window {
    refine: {
      api: (method: string, path: string, body?: unknown) => Promise<ApiResponse>
      backendState: () => Promise<BackendState>
      restartBackend: () => Promise<BackendState>
      onBackendState: (listener: (state: BackendState) => void) => () => void
      pickFolder: (start?: string) => Promise<string | null>
      openExternal: (url: string) => Promise<void>
    }
  }
}
