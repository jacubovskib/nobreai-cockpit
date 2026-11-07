import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { patch, post } from '../../api/client'
import type { CardDetail } from '../../api/types'
import { ErrorLine, btnPrimary, h2, input, muted, panel } from './shared'

export function Todos({ card }: { card: CardDetail }) {
  const qc = useQueryClient()
  const [text, setText] = useState('')
  const refresh = () => qc.invalidateQueries({ queryKey: ['card', card.key] })
  const add = useMutation({
    mutationFn: () => post(`/api/cards/${card.key}/todos`, { text }),
    onSuccess: async () => { setText(''); await refresh() },
  })
  const toggle = useMutation({
    mutationFn: (v: { id: number; done: boolean }) => patch(`/api/todos/${v.id}`, { done: v.done }),
    onSuccess: refresh,
  })
  const todos = card.content.todos
  return (
    <section aria-labelledby="todo-t" style={panel}>
      <h2 id="todo-t" style={h2}>Feito e falta</h2>
      {todos.length === 0 && <p style={{ ...muted, margin: 0 }}>Nenhum item ainda.</p>}
      <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 8 }}>
        {todos.map((t) => (
          <li key={t.id} style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
            <input
              type="checkbox"
              aria-label={t.text}
              checked={t.done}
              disabled={toggle.isPending}
              onChange={(e) => toggle.mutate({ id: t.id, done: e.target.checked })}
              style={{ width: 18, height: 18 }}
            />
            <span style={{ color: t.done ? 'var(--text-2)' : 'var(--text)' }}>{t.text}</span>
          </li>
        ))}
      </ul>
      <ErrorLine error={toggle.error} />
      <form
        style={{ display: 'flex', gap: 8 }}
        onSubmit={(e) => { e.preventDefault(); if (text.trim()) add.mutate() }}
      >
        <input style={{ ...input, flex: 1 }} aria-label="Novo item" placeholder="Novo item…" value={text} onChange={(e) => setText(e.target.value)} />
        <button type="submit" style={btnPrimary} disabled={add.isPending}>Adicionar item</button>
      </form>
      <ErrorLine error={add.error} />
    </section>
  )
}
