export type UserRole = 'passenger' | 'driver' | 'country_admin' | 'super_admin'

export interface User {
  id: number
  full_name: string
  email?: string | null
  phone?: string | null
  role: UserRole
  status: string
  country_id?: number | null
}

export interface Country {
  id: number
  name: string
  iso2: string
  iso3?: string
  currency_code?: string
  phone_code?: string
  is_active?: boolean
}

const API_BASE = (import.meta.env.VITE_API_URL || 'http://localhost:8080/api').replace(/\/$/, '')

export class ApiError extends Error {
  readonly status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

export function getToken() {
  return localStorage.getItem('travo_token')
}

export async function request<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const headers = new Headers(options.headers)
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)

  if (options.body && !(options.body instanceof FormData)) {
    headers.set('Content-Type', 'application/json')
  }

  let response: Response
  try {
    response = await fetch(`${API_BASE}${path}`, { ...options, headers })
  } catch {
    throw new Error('Could not reach Travo. Check the API URL and try again.')
  }

  const data = await response.json().catch(() => ({})) as { error?: string }
  if (!response.ok) {
    throw new ApiError(data.error || `Request failed (${response.status})`, response.status)
  }
  return data as T
}

export function post<T>(path: string, body?: unknown) {
  return request<T>(path, {
    method: 'POST',
    body: body === undefined ? undefined : JSON.stringify(body),
  })
}

export function patch<T>(path: string, body: unknown) {
  return request<T>(path, { method: 'PATCH', body: JSON.stringify(body) })
}

export function remove<T>(path: string) {
  return request<T>(path, { method: 'DELETE' })
}

export async function download(path: string) {
  const headers = new Headers()
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const response = await fetch(`${API_BASE}${path}`, { headers })
  if (!response.ok) {
    const data = await response.json().catch(() => ({})) as { error?: string }
    throw new ApiError(data.error || `Download failed (${response.status})`, response.status)
  }
  return response.blob()
}
