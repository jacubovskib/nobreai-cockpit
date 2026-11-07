import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { patch } from '../../api/client'
import { ErrorLine, btn, btnPrimary, input, muted } from './shared'

// Campo de texto do card editável pelo humano: PATCH /api/cards/{key} {field: valor}.
export function EditableText({
  cardKey, field, label, value, multiline = true, empty = 'Ainda não preenchido.', render,
}: {
  cardKey: string
  field: 'where_stopped' | 'goal' | 'done_when' | 'how_to_test'
  label: string
  value: string
  multiline?: boolean
  empty?: string
  render?: (v: string) => React.ReactNode
}) {
  const qc = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(value)
  const save = useMutation({
    mutationFn: () => patch(`/api/cards/${cardKey}`, { [field]: draft }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['card', cardKey] })
      setEditing(false)
    },
  })
  if (!editing) {
    return (
      <div style={{ display: 'flex', gap: 12, alignItems: 'flex-start', justifyContent: 'space-between' }}>
        <div style={{ flex: 1, minWidth: 0, whiteSpace: 'pre-wrap' }}>
          {value ? (render ? render(value) : value) : <span style={muted}>{empty}</span>}
        </div>
        <button type="button" style={btn} aria-label={`Editar ${label}`} onClick={() => { setDraft(value); save.reset(); setEditing(true) }}>
          Editar
        </button>
      </div>
    )
  }
  return (
    <form
      style={{ display: 'flex', flexDirection: 'column', gap: 8 }}
      onSubmit={(e) => { e.preventDefault(); save.mutate() }}
    >
      <label style={{ display: 'flex', flexDirection: 'column', gap: 4, ...muted }}>
        {label}
        {multiline ? (
          <textarea rows={4} style={{ ...input, padding: 10 }} value={draft} onChange={(e) => setDraft(e.target.value)} />
        ) : (
          <input style={input} value={draft} onChange={(e) => setDraft(e.target.value)} />
        )}
      </label>
      <ErrorLine error={save.error} />
      <div style={{ display: 'flex', gap: 8 }}>
        <button type="submit" style={btnPrimary} disabled={save.isPending}>Salvar {label}</button>
        <button type="button" style={btn} onClick={() => setEditing(false)}>Cancelar</button>
      </div>
    </form>
  )
}
