import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ApiError, post } from '../../api/client'
import { usePrompts } from '../../api/queries'
import { agentInfo } from '../../ui/agents'
import { ErrorLine, btnPrimary, h2, input, muted, panel } from './shared'

const AGENT_OPTIONS = [
  { id: 'claude', enabled: true },
  { id: 'codex', enabled: false },
  { id: 'kiro', enabled: false },
]
const PLATFORM_MODE = 'platform'
const BACKGROUND_MODE = 'background'

const NO_PROMPT = 'nenhum'

// `enabled` liga o botão (Claude Code no piloto). Sessão viva no card volta
// 409; a mensagem aparece em linha com "Forçar mesmo assim", que reenvia com force.
export function ContinuePanel({ cardKey, enabled = false, onTerminal }: { cardKey: string; enabled?: boolean; onTerminal?: (id: string) => void }) {
  const qc = useQueryClient()
  const prompts = usePrompts()
  const [agent, setAgent] = useState('claude')
  const [mode, setMode] = useState(PLATFORM_MODE)
  const [slug, setSlug] = useState<string | null>(null)
  const list = prompts.data ?? []
  const effectiveSlug = slug ?? list.find((p) => p.is_default_handoff)?.slug ?? ''
  const name = agentInfo(agent).name
  const start = useMutation({
    mutationFn: (force: boolean) =>
      post<{ terminal_id?: string }>(`/api/cards/${cardKey}/continue`, { agent, mode, prompt_slug: effectiveSlug, ...(force ? { force: true } : {}) }),
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: ['card', cardKey] })
      if (res?.terminal_id) {
        qc.invalidateQueries({ queryKey: ['terminals'] })
        onTerminal?.(res.terminal_id)
      }
    },
  })
  const liveConflict = start.error instanceof ApiError && start.error.status === 409 && (start.error.body as { code?: string } | null)?.code === 'live_session'
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
          <option value={NO_PROMPT}>Nenhum: só o pacote de retomada</option>
        </select>
      </label>
      <label style={{ display: 'flex', flexDirection: 'column', gap: 4, ...muted }}>
        Abrir em
        <select style={input} value={mode} onChange={(e) => setMode(e.target.value)}>
          <option value={PLATFORM_MODE}>Padrão da plataforma (definido em Configuração)</option>
          <option value="tmux" disabled>Sessão tmux (em breve)</option>
          <option value={BACKGROUND_MODE}>Em segundo plano, com saída aqui na interface</option>
        </select>
      </label>
      <div style={{ display: 'flex', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
        <button type="button" style={{ ...btnPrimary, opacity: enabled ? 1 : 0.5 }} disabled={!enabled || start.isPending} onClick={() => start.mutate(false)}>
          Iniciar {name} neste card
        </button>
        {!enabled && <span style={muted}>disponível na próxima etapa</span>}
      </div>
      <ErrorLine error={start.error} />
      {liveConflict && (
        <div>
          <button type="button" style={btnPrimary} disabled={start.isPending} onClick={() => start.mutate(true)}>
            Forçar mesmo assim
          </button>
        </div>
      )}
      {start.isSuccess && <p role="status" style={{ ...muted, margin: 0 }}>{start.data?.terminal_id ? 'Agente iniciado em segundo plano — terminal abaixo.' : 'Terminal aberto.'}</p>}
      <p style={{ ...muted, margin: 0 }}>
        {mode === BACKGROUND_MODE
          ? 'Inicia o agente em segundo plano, no diretório do projeto, no branch do card, com o pacote de retomada como contexto. A saída aparece no painel Terminal deste card. A sessão atual, se houver, não é encerrada.'
          : 'Segue o modo padrão da Configuração: abre uma nova janela do terminal ou inicia em segundo plano, no diretório do projeto, no branch do card, com o pacote de retomada como contexto. A sessão atual, se houver, não é encerrada.'}
      </p>
    </section>
  )
}
