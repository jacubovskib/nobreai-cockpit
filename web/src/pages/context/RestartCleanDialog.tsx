import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { post } from '../../api/client'
import { useWithProject } from '../../app/project'
import { ErrorLine, btn, btnPrimary, h2, input, muted, panel } from '../card/shared'

// O servidor recusa um "Onde parei" mais velho que isto.
export const WHERE_STOPPED_MAX_MIN = 30

interface Props {
  sessionId: string
  whereStopped: string
  whereStoppedAt: string | null
  onClose: () => void
  now?: number
}

// Painel (sem diálogo do navegador) para recomeçar limpo: mostra o "Onde parei"
// do card e só envia o texto se foi editado ou se está velho/vazio.
export function RestartCleanDialog({ sessionId, whereStopped, whereStoppedAt, onClose, now = Date.now() }: Props) {
  const qc = useQueryClient()
  const withP = useWithProject()
  const [text, setText] = useState(whereStopped)
  const [serverAsked, setServerAsked] = useState(false)
  const old = !whereStoppedAt || now - Date.parse(whereStoppedAt) > WHERE_STOPPED_MAX_MIN * 60000
  const edited = text.trim() !== whereStopped.trim()
  const needsText = old || serverAsked
  const send = useMutation({
    mutationFn: () => post<{ terminal_id?: string }>(`/api/sessions/${encodeURIComponent(sessionId)}/restart-clean`, edited || needsText ? { where_stopped: text.trim() } : {}),
    onError: (e: Error) => { if (/registre onde parou/.test(e.message)) setServerAsked(true) },
    onSettled: () => Promise.all(['card', 'cards', 'sessions', 'context-live', 'session-context', 'terminals', 'overview'].map((k) => qc.invalidateQueries({ queryKey: [k] }))),
  })
  const done = send.isSuccess
  return (
    <section aria-labelledby="recomecar-t" style={{ ...panel, background: 'var(--bg)' }}>
      <h3 id="recomecar-t" style={h2}>Recomeçar limpo</h3>
      {done ? (
        <>
          <p role="status" style={{ margin: 0 }}>Terminal novo aberto.</p>
          {send.data?.terminal_id && <div><Link to={withP(`/terminals?id=${encodeURIComponent(send.data.terminal_id)}`)} style={btn}>Ver terminal</Link></div>}
          <div><button type="button" style={btn} onClick={onClose}>Fechar</button></div>
        </>
      ) : (
        <>
          <p style={{ ...muted, margin: 0 }}>A sessão atual segue viva, mas sem o card. O agente novo começa do “Onde parei” abaixo.</p>
          <label style={{ display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13, color: 'var(--text-2)' }}>
            Onde parei
            <textarea style={{ ...input, minHeight: 120, padding: 10, resize: 'vertical' }} value={text} onChange={(e) => setText(e.target.value)} />
          </label>
          {needsText && <p style={{ ...muted, margin: 0, color: 'var(--warn)' }}>O registro está velho ou vazio: confirme ou atualize o texto antes de recomeçar.</p>}
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            <button type="button" style={btnPrimary} disabled={send.isPending || (needsText && text.trim() === '')} onClick={() => send.mutate()}>
              {send.isPending ? 'Abrindo…' : 'Recomeçar limpo'}
            </button>
            <button type="button" style={btn} onClick={onClose}>Cancelar</button>
          </div>
          <ErrorLine error={send.error} />
        </>
      )}
    </section>
  )
}
