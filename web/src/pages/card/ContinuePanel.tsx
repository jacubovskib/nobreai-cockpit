import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { post } from '../../api/client'
import { usePrompts } from '../../api/queries'
import { agentInfo } from '../../ui/agents'
import { ErrorLine, btnPrimary, h2, input, muted, panel } from './shared'

const AGENT_OPTIONS = [
  { id: 'claude', enabled: true },
  { id: 'codex', enabled: false },
  { id: 'kiro', enabled: false },
]
const PLATFORM_MODE = 'platform'

// `enabled` fica falso até a rota POST /api/cards/{key}/continue existir
// (Task 12); a lógica de envio e de erro 409 em linha já está pronta.
export function ContinuePanel({ cardKey, enabled = false }: { cardKey: string; enabled?: boolean }) {
  const qc = useQueryClient()
  const prompts = usePrompts()
  const [agent, setAgent] = useState('claude')
  const [mode, setMode] = useState(PLATFORM_MODE)
  const [slug, setSlug] = useState<string | null>(null)
  const list = prompts.data ?? []
  const effectiveSlug = slug ?? list.find((p) => p.is_default_handoff)?.slug ?? ''
  const name = agentInfo(agent).name
  const start = useMutation({
    mutationFn: () => post(`/api/cards/${cardKey}/continue`, { agent, mode, prompt_slug: effectiveSlug }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['card', cardKey] }),
  })
  return (
    <section aria-labelledby="cont-t" style={panel}>
      <h2 id="cont-t" style={h2}>Continuar com outro agente</h2>
      <div role="radiogroup" aria-label="Agente" style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        {AGENT_OPTIONS.map((a) => {
          const info = agentInfo(a.id)
          return (
            <button
              key={a.id}
              type="button"
              role="radio"
              aria-checked={agent === a.id}
              disabled={!a.enabled}
              onClick={() => setAgent(a.id)}
              style={{ display: 'flex', gap: 10, alignItems: 'center', minHeight: 44, padding: '0 12px', borderRadius: 8, textAlign: 'left', font: 'inherit', color: 'var(--text)', background: agent === a.id ? '#1F232C' : 'transparent', border: `1px solid ${agent === a.id ? info.color : '#3A3F4B'}`, cursor: a.enabled ? 'pointer' : 'not-allowed', opacity: a.enabled ? 1 : 0.6 }}
            >
              <span aria-hidden="true" style={{ width: 10, height: 10, borderRadius: 3, background: info.color }} />
              <span>{info.name}</span>
              {!a.enabled && <span style={muted}>em breve</span>}
            </button>
          )
        })}
      </div>
      <label style={{ display: 'flex', flexDirection: 'column', gap: 4, ...muted }}>
        Prompt inicial
        <select style={input} value={effectiveSlug} onChange={(e) => setSlug(e.target.value)}>
          {list.map((p) => <option key={p.slug} value={p.slug}>{p.name}{p.is_default_handoff ? ' (padrão)' : ''}</option>)}
        </select>
      </label>
      <label style={{ display: 'flex', flexDirection: 'column', gap: 4, ...muted }}>
        Abrir em
        <select style={input} value={mode} onChange={(e) => setMode(e.target.value)}>
          <option value={PLATFORM_MODE}>Padrão da plataforma: nova janela do terminal</option>
          <option value="tmux" disabled>Sessão tmux (em breve)</option>
          <option value="background" disabled>Em segundo plano, com saída aqui na interface (em breve)</option>
        </select>
      </label>
      <div style={{ display: 'flex', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
        <button type="button" style={{ ...btnPrimary, opacity: enabled ? 1 : 0.5 }} disabled={!enabled || start.isPending} onClick={() => start.mutate()}>
          Iniciar {name} neste card
        </button>
        {!enabled && <span style={muted}>disponível na próxima etapa</span>}
      </div>
      <ErrorLine error={start.error} />
      <p style={{ ...muted, margin: 0 }}>
        A sessão atual recebe um pedido para registrar “Onde parei” e encerrar. O novo agente abre no branch do card com o pacote de retomada como contexto.
      </p>
    </section>
  )
}
