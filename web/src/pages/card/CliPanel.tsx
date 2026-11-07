import { useState } from 'react'
import { btn, h2, muted, panel } from './shared'

export function CliPanel({ cardKey, onTogglePreview, previewOpen }: { cardKey: string; onTogglePreview: () => void; previewOpen: boolean }) {
  const [msg, setMsg] = useState('')
  const cmd = `nobre resume ${cardKey}`
  async function copy() {
    try {
      await navigator.clipboard.writeText(cmd)
      setMsg('Comando copiado.')
    } catch {
      setMsg('Não foi possível copiar automaticamente. Selecione o comando e copie.')
    }
  }
  return (
    <section aria-labelledby="cli-t" style={panel}>
      <h2 id="cli-t" style={h2}>Pelo terminal</h2>
      <p style={{ ...muted, margin: 0 }}>Em qualquer agente, dentro do repositório:</p>
      <div style={{ display: 'flex', gap: 8, alignItems: 'center', justifyContent: 'space-between', padding: '8px 12px', borderRadius: 8, background: 'var(--bg)', border: '1px solid var(--border)' }}>
        <code className="mono">{cmd}</code>
        <button type="button" style={btn} aria-label="Copiar comando" onClick={copy}>Copiar</button>
      </div>
      {msg && <p role="status" style={{ ...muted, margin: 0 }}>{msg}</p>}
      <p style={{ ...muted, margin: 0 }}>Ou só diga ao agente: “continue o {cardKey}”. A skill do Cockpit faz o resto.</p>
      <button type="button" style={btn} aria-expanded={previewOpen} onClick={onTogglePreview}>
        {previewOpen ? 'Ocultar o pacote de retomada' : 'Ver o pacote de retomada'}
      </button>
    </section>
  )
}
