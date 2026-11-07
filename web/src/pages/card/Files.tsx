import { useState } from 'react'
import type { CardDetail } from '../../api/types'
import { btn, h2, muted, panel } from './shared'

export function Files({ card }: { card: CardDetail }) {
  const [open, setOpen] = useState(false)
  const files = card.content.files
  const touched = card.touched_files
  const edited = touched.filter((t) => t.kind === 'editado').length
  const read = touched.length - edited
  return (
    <section aria-labelledby="arq-t" style={panel}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap' }}>
        <h2 id="arq-t" style={h2}>Arquivos essenciais</h2>
        <span style={muted}>Marcados pelo agente, com motivo</span>
      </div>
      {files.length === 0 && <p style={{ ...muted, margin: 0 }}>Nenhum arquivo marcado.</p>}
      {files.map((f) => (
        <div key={f.id} style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <span>
            <span className="mono">{f.path}</span> <span className="mono" style={muted}>{f.lines}</span>
          </span>
          {f.why && <span style={muted}>{f.why}</span>}
        </div>
      ))}
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'center', borderTop: '1px solid var(--border)', paddingTop: 12, flexWrap: 'wrap' }}>
        <span style={muted}>
          <strong style={{ color: 'var(--text)' }}>{touched.length} arquivos tocados</strong> registrados pelos hooks ({edited} editados, {read} só lidos)
        </span>
        {touched.length > 0 && (
          <button type="button" style={btn} aria-expanded={open} onClick={() => setOpen(!open)}>
            {open ? 'Ocultar arquivos tocados' : 'Ver arquivos tocados'}
          </button>
        )}
      </div>
      {open && (
        <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 4 }}>
          {touched.map((t) => (
            <li key={t.path} style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
              <span className="mono">{t.path}</span>
              <span style={muted}>{t.kind}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
