import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import type { Todo } from './api'

const todo = (id: number, title: string, done = false): Todo => ({
  id,
  title,
  done,
  created_at: '2026-10-10T09:00:00Z',
  updated_at: '2026-10-10T09:00:00Z',
})

const json = (status: number, body: unknown) =>
  new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

let fetchMock: ReturnType<typeof vi.fn>

beforeEach(() => {
  fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('App', () => {
  it('lists todos from the API on the same origin', async () => {
    fetchMock.mockResolvedValueOnce(json(200, [todo(1, 'write the migration', true), todo(2, 'deploy the API')]))
    render(<App />)

    expect(await screen.findByText('write the migration')).toBeTruthy()
    expect(screen.getByText('deploy the API')).toBeTruthy()
    expect(fetchMock).toHaveBeenCalledWith('/api/todos', expect.anything())
  })

  it('adds a todo with a trimmed title', async () => {
    fetchMock.mockResolvedValueOnce(json(200, []))
    render(<App />)
    await screen.findByText('Nothing to do yet.')

    fetchMock.mockResolvedValueOnce(json(201, todo(3, 'verify isolation')))
    fireEvent.change(screen.getByLabelText('New todo'), { target: { value: '  verify isolation  ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))

    expect(await screen.findByText('verify isolation')).toBeTruthy()
    const [url, init] = fetchMock.mock.calls[1]
    expect(url).toBe('/api/todos')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body)).toEqual({ title: 'verify isolation' })
  })

  it('toggles and deletes a todo', async () => {
    fetchMock.mockResolvedValueOnce(json(200, [todo(4, 'rotate credentials')]))
    render(<App />)
    const checkbox = await screen.findByRole<HTMLInputElement>('checkbox')

    fetchMock.mockResolvedValueOnce(json(200, todo(4, 'rotate credentials', true)))
    fireEvent.click(checkbox)
    await waitFor(() => expect(checkbox.checked).toBe(true))
    expect(fetchMock.mock.calls[1][0]).toBe('/api/todos/4')
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ done: true })

    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }))
    fireEvent.click(screen.getByRole('button', { name: 'Delete "rotate credentials"' }))
    expect(await screen.findByText('Nothing to do yet.')).toBeTruthy()
    expect(fetchMock.mock.calls[2][1].method).toBe('DELETE')
  })

  it('shows the API error message', async () => {
    fetchMock.mockResolvedValueOnce(json(503, { error: 'database unavailable' }))
    render(<App />)

    expect((await screen.findByRole('alert')).textContent).toBe('database unavailable')
  })

  it('falls back to the status line when the error is not JSON', async () => {
    fetchMock.mockResolvedValueOnce(new Response('<html>bad gateway</html>', { status: 502, statusText: 'Bad Gateway' }))
    render(<App />)

    expect((await screen.findByRole('alert')).textContent).toBe('502 Bad Gateway')
  })
})
