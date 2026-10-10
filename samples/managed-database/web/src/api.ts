// The API is served on the same host under /api, so every call is a relative
// URL. The browser never learns where the database is or how to log in to it.

export interface Todo {
  id: number
  title: string
  done: boolean
  created_at: string
  updated_at: string
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: init?.body ? { 'Content-Type': 'application/json' } : undefined,
  })
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`.trim()
    try {
      const body = (await res.json()) as { error?: string }
      if (body.error) message = body.error
    } catch {
      // Not JSON (a proxy error page, for example): keep the status line.
    }
    throw new Error(message)
  }
  return (res.status === 204 ? undefined : await res.json()) as T
}

export const listTodos = () => request<Todo[]>('/api/todos')

export const createTodo = (title: string) =>
  request<Todo>('/api/todos', { method: 'POST', body: JSON.stringify({ title }) })

export const updateTodo = (id: number, patch: { title?: string; done?: boolean }) =>
  request<Todo>(`/api/todos/${id}`, { method: 'PATCH', body: JSON.stringify(patch) })

export const deleteTodo = (id: number) =>
  request<void>(`/api/todos/${id}`, { method: 'DELETE' })
