import { useId } from 'react'
import { ErrorLine, btn, btnPrimary, input, muted } from '../card/shared'
import { useRestartClean } from '../context/useRestartClean'

// Painel "Limpar e continuar" do tile: cobre o corpo (o terminal segue montado
// por baixo) e pede o recomeço em segundo plano para a sessão em que abriu.

export const overlay: React.CSSProperties = {
  position: 'absolute', inset: 0, overflow: 'auto', zIndex: 4, background: 'var(--surface, #14171D)',
  display: 'flex', flexDirection: 'column', gap: 10, padding: 12,
}

export interface RestartPanelProps {
  sessionId: string; whereStopped: string; whereStoppedAt: string | null
  onDone(terminalId: string | undefined): void
  onCancel(): void
}

export function RestartPanel(p: RestartPanelProps) {
  const titleId = useId() // vários tiles podem ter o painel aberto ao mesmo tempo
  const r = useRestartClean({ ...p, mode: 'background' })
  return (
    <section aria-labelledby={titleId} style={overlay}>
      <h3 id={titleId} style={{ margin: 0, fontSize: 14, fontWeight: 600 }}>Recomeçar limpo</h3>
      <p style={{ ...muted, margin: 0 }}>A sessão atual segue viva, mas sem o card. O agente novo começa do “Onde parei” abaixo, com o contexto zerado.</p>
      <label style={{ display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13, color: 'var(--text-2)' }}>
        Onde parei
        <textarea style={{ ...input, minHeight: 90, padding: 10, resize: 'vertical' }} value={r.text} onChange={(e) => r.setText(e.target.value)} />
      </label>
      {r.needsText && <p style={{ ...muted, margin: 0, color: 'var(--warn)' }}>O registro está velho ou vazio: confirme ou atualize o texto antes de recomeçar.</p>}
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        <button type="button" style={btnPrimary} disabled={!r.canSend} onClick={r.send}>
          {r.pending ? 'Abrindo…' : 'Limpar e continuar'}
        </button>
        <button type="button" style={btn} onClick={p.onCancel}>Cancelar</button>
      </div>
      <ErrorLine error={r.error} />
    </section>
  )
}
