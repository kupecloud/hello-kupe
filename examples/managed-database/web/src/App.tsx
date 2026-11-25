import { useEffect, useState, type FormEvent } from 'react'
import { createTodo, deleteTodo, listTodos, updateTodo, type Todo } from './api'

// Matches the API's limit and the CHECK constraint on todos.title.
const MAX_TITLE = 200

const errorMessage = (err: unknown) => (err instanceof Error ? err.message : String(err))

export default function App() {
  const [todos, setTodos] = useState<Todo[]>([])
  const [title, setTitle] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    listTodos()
      .then((items) => !cancelled && setTodos(items))
      .catch((err: unknown) => !cancelled && setError(errorMessage(err)))
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [])

  async function attempt(action: () => Promise<void>) {
    setError(null)
    try {
      await action()
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  function add(event: FormEvent) {
    event.preventDefault()
    const trimmed = title.trim()
    if (!trimmed) return
    void attempt(async () => {
      const created = await createTodo(trimmed)
      setTodos((prev) => [...prev, created])
      setTitle('')
    })
  }

  const toggle = (todo: Todo) =>
    attempt(async () => {
      const updated = await updateTodo(todo.id, { done: !todo.done })
      setTodos((prev) => prev.map((t) => (t.id === updated.id ? updated : t)))
    })

  const remove = (todo: Todo) =>
    attempt(async () => {
      await deleteTodo(todo.id)
      setTodos((prev) => prev.filter((t) => t.id !== todo.id))
    })

  return (
    <main>
      <h1>Todos</h1>
      <p className="lede">Stored in a Kupe managed PostgreSQL database, read and written by the API.</p>

      <form onSubmit={add}>
        <label htmlFor="title" className="visually-hidden">
          New todo
        </label>
        <input
          id="title"
          value={title}
          maxLength={MAX_TITLE}
          placeholder="What needs doing?"
          onChange={(e) => setTitle(e.target.value)}
        />
        <button type="submit" disabled={!title.trim()}>
          Add
        </button>
      </form>

      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}

      {loading ? (
        <p>Loading…</p>
      ) : todos.length === 0 ? (
        <p className="empty">Nothing to do yet.</p>
      ) : (
        <ul>
          {todos.map((todo) => (
            <li key={todo.id} className={todo.done ? 'done' : undefined}>
              <label>
                <input type="checkbox" checked={todo.done} onChange={() => void toggle(todo)} />
                <span>{todo.title}</span>
              </label>
              <button type="button" aria-label={`Delete "${todo.title}"`} onClick={() => void remove(todo)}>
                Delete
              </button>
            </li>
          ))}
        </ul>
      )}
    </main>
  )
}
