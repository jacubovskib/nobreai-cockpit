import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { patch } from '../../api/client'
import { useCards } from '../../api/queries'
import type { CardDetail, CardType, Priority } from '../../api/types'
import { PRIORITY_LABEL, TYPE_LABEL } from '../boardModel'
import { ErrorLine, btnPrimary, input, muted } from './shared'

const field: React.CSSProperties = { display: 'flex', flexDirection: 'column', gap: 6, ...muted }

// splitLabels: texto livre separado por vírgula ou espaço vira a lista de labels.
export const splitLabels = (s: string): string[] => s.split(/[,\s]+/).filter(Boolean)

// Hierarquia e classificação do card: tipo, épico, prioridade, labels e pontos.
// Envia só o que mudou (labels, quando mudam, vão como lista completa).
export function Hierarchy({ card }: { card: CardDetail }) {
  const qc = useQueryClient()
  const cards = useCards().data
  const type = card.type ?? 'tarefa'
  const priority = card.priority ?? 'media'
  const labels = (card.labels ?? []).join(', ')
  const points = card.points == null ? '' : String(card.points)
  const parent = card.parent_key ?? ''
  const [t, setT] = useState<CardType>(type)
  const [p, setP] = useState<Priority>(priority)
  const [epic, setEpic] = useState(parent)
  const [lab, setLab] = useState(labels)
  const [pts, setPts] = useState(points)

  const epics = (cards ?? []).filter((c) => c.type === 'epico' && c.column !== 'concluido' && c.key !== card.key)
  const hasParent = parent !== '' && epics.some((c) => c.key === parent)
  const save = useMutation({
    mutationFn: () => {
      const body: Record<string, unknown> = {}
      if (t !== type) body.type = t
      if (p !== priority) body.priority = p
      if (t !== 'epico' && epic !== parent) body.parent_key = epic
      if (splitLabels(lab).join(' ') !== splitLabels(labels).join(' ')) body.labels = splitLabels(lab)
      if (pts.trim() !== points) {
        const n = Number(pts.trim().replace(',', '.'))
        if (pts.trim() !== '' && (!Number.isFinite(n) || n < 0)) throw new Error('points precisa ser um número maior ou igual a zero')
        body.points = pts.trim() === '' ? null : n
      }
      return patch(`/api/cards/${card.key}`, body)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['card', card.key] })
      qc.invalidateQueries({ queryKey: ['cards'] })
    },
  })
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'flex-end', gap: 10 }}>
      <label style={field}>Tipo
        <select style={{ ...input, minHeight: 36 }} value={t} onChange={(e) => setT(e.target.value as CardType)}>
          {(Object.keys(TYPE_LABEL) as CardType[]).map((k) => <option key={k} value={k}>{TYPE_LABEL[k]}</option>)}
        </select>
      </label>
      {t !== 'epico' && (
        <label style={field}>Épico
          <select style={{ ...input, minHeight: 36 }} value={epic} onChange={(e) => setEpic(e.target.value)}>
            <option value="">sem épico</option>
            {parent !== '' && !hasParent && <option value={parent}>{parent} · {card.parent_title}</option>}
            {epics.map((c) => <option key={c.key} value={c.key}>{c.key} · {c.title}</option>)}
          </select>
        </label>
      )}
      <label style={field}>Prioridade
        <select style={{ ...input, minHeight: 36 }} value={p} onChange={(e) => setP(e.target.value as Priority)}>
          {(Object.keys(PRIORITY_LABEL) as Priority[]).map((k) => <option key={k} value={k}>{PRIORITY_LABEL[k]}</option>)}
        </select>
      </label>
      <label style={field}>Labels
        <input style={{ ...input, minHeight: 36, width: 180 }} placeholder="api, urgente" value={lab} onChange={(e) => setLab(e.target.value)} />
      </label>
      <label style={field}>Pontos
        <input style={{ ...input, minHeight: 36, width: 80 }} inputMode="decimal" value={pts} onChange={(e) => setPts(e.target.value)} />
      </label>
      <button type="button" style={btnPrimary} disabled={save.isPending} onClick={() => save.mutate()}>Salvar classificação</button>
      <ErrorLine error={save.error} />
    </div>
  )
}
