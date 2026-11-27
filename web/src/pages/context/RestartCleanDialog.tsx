import { Link } from 'react-router-dom'
import { useWithProject } from '../../app/project'
import { ErrorLine, btn, btnPrimary, h2, input, muted, panel } from '../card/shared'
import { WHERE_STOPPED_MAX_MIN, useRestartClean } from './useRestartClean'

export { WHERE_STOPPED_MAX_MIN }

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
  const withP = useWithProject()
  const r = useRestartClean({ sessionId, whereStopped, whereStoppedAt, now })
  return (
    <section aria-labelledby="recomecar-t" style={{ ...panel, background: 'var(--bg)' }}>
      <h3 id="recomecar-t" style={h2}>Recomeçar limpo</h3>
      {r.done ? (
        <>
          <p role="status" style={{ margin: 0 }}>Terminal novo aberto.</p>
          {r.terminalId && <div><Link to={withP(`/terminals?id=${encodeURIComponent(r.terminalId)}`)} style={btn}>Ver terminal</Link></div>}
          <div><button type="button" style={btn} onClick={onClose}>Fechar</button></div>
        </>
      ) : (
        <>
          <p style={{ ...muted, margin: 0 }}>A sessão atual segue viva, mas sem o card. O agente novo começa do “Onde parei” abaixo.</p>
          <label style={{ display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13, color: 'var(--text-2)' }}>
            Onde parei
            <textarea style={{ ...input, minHeight: 120, padding: 10, resize: 'vertical' }} value={r.text} onChange={(e) => r.setText(e.target.value)} />
          </label>
          {r.needsText && <p style={{ ...muted, margin: 0, color: 'var(--warn)' }}>O registro está velho ou vazio: confirme ou atualize o texto antes de recomeçar.</p>}
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            <button type="button" style={btnPrimary} disabled={!r.canSend} onClick={r.send}>
              {r.pending ? 'Abrindo…' : 'Recomeçar limpo'}
            </button>
            <button type="button" style={btn} onClick={onClose}>Cancelar</button>
          </div>
          <ErrorLine error={r.error} />
        </>
      )}
    </section>
  )
}
