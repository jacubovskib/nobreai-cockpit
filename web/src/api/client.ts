export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

// api chama o nobred com URL relativa. Toda requisição com corpo (e todo
// método fora GET) leva Content-Type: application/json, senão o daemon
// responde 415.
export async function api<T>(path: string, init: { method?: string; body?: unknown } = {}): Promise<T> {
  const method = init.method ?? 'GET'
  const headers: Record<string, string> = {}
  if (method !== 'GET' && method !== 'HEAD') headers['Content-Type'] = 'application/json'
  const res = await fetch(path, {
    method,
    headers,
    body: init.body === undefined ? undefined : JSON.stringify(init.body),
  })
  const text = await res.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = null
    }
  }
  if (!res.ok) {
    const msg = (data as { error?: string } | null)?.error ?? `erro ${res.status}`
    throw new ApiError(res.status, msg)
  }
  return data as T
}

export const get = <T>(path: string) => api<T>(path)
export const post = <T>(path: string, body?: unknown) => api<T>(path, { method: 'POST', body: body ?? {} })
export const put = <T>(path: string, body: unknown) => api<T>(path, { method: 'PUT', body })
export const patch = <T>(path: string, body: unknown) => api<T>(path, { method: 'PATCH', body })
export const del = <T>(path: string) => api<T>(path, { method: 'DELETE' })
