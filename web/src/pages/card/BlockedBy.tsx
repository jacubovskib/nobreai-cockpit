import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { patch } from '../../api/client'
import type { Card } from '../../api/types'
import { ErrorLine, btnPrimary, input, muted } from './shared'

// Bloqueado por: chave do card que precisa terminar antes (vazio limpa).
export function BlockedBy({ card }: { card: Card }) {
  const qc = useQueryClient()
  const [v, setV] = useState(card.blocked_by ?? '')
  const save = useMutation({
    mutationFn: () => patch(`/api/cards/${card.key}`, { blocked_by: v.trim() }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['card', card.key] })
      qc.invalidateQueries({ queryKey: ['cards'] })
    },
  })
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'flex-end', gap: 10 }}>
      <label style={{ display: 'flex', flexDirection: 'column', gap: 6, ...muted }}>Bloqueado por
        <input style={{ ...input, minHeight: 36, width: 140 }} className="mono" placeholder="NAC-4" value={v} onChange={(e) => setV(e.target.value)} />
      </label>
      <button type="button" style={btnPrimary} disabled={save.isPending} onClick={() => save.mutate()}>Salvar bloqueio</button>
      <ErrorLine error={save.error} />
    </div>
  )
}
