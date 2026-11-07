import { useState, type CSSProperties, type ReactNode } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { del, patch, post, put } from '../api/client'
import { useCards, useLimits, usePrices, useProjects, useSettings, useTotals } from '../api/queries'
import type { Limit, Price, Project, Settings } from '../api/types'
import { agentInfo } from '../ui/agents'
import { AgentSwatch } from '../ui/AgentSwatch'
import { ErrorLine, btn, btnPrimary, h2, input, muted, panel } from './card/shared'

const AGENT_IDS = ['claude', 'codex', 'kiro']
const RULES: [string, string][] = [
  ['goal', 'Objetivo e “pronto quando” preenchidos'],
  ['where_stopped', '“Onde parei” atualizado recentemente'],
  ['essential_file', 'Pelo menos 1 arquivo essencial com motivo'],
  ['todo_open', 'Lista “Falta” não está vazia'],
  ['how_to_test', '“Como testar” preenchido'],
  ['no_open_questions', 'Nenhuma pergunta em aberto sem resposta'],
]
const SETUP_CHECKS: Record<string, string[]> = {
  claude: ['CLI encontrado', 'Hooks instalados', 'Skill do Cockpit instalada', 'Recebendo eventos'],
  codex: ['CLI encontrado', 'Hooks instalados', 'Instruções no AGENTS.md atualizadas', 'Uso de tokens'],
  kiro: ['CLI encontrado', 'Hook de arquivo editado', 'Steering do Cockpit instalado', 'Recebendo eventos'],
}
const NAV = [
  ['agentes', 'Agentes e setup'], ['limites', 'Limites de custo'], ['cards', 'Criação de cards'],
  ['handoff', 'Pronto para handoff'], ['projetos', 'Projetos'], ['precos', 'Preços e moeda'],
]

const th: CSSProperties = { padding: '8px 12px', fontWeight: 500 }
const td: CSSProperties = { padding: '8px 12px' }
const thead: CSSProperties = { textAlign: 'left', color: 'var(--text-2)', fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.06em' }
const field: CSSProperties = { display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13, color: 'var(--text-2)' }
const check: CSSProperties = { display: 'flex', alignItems: 'flex-start', gap: 10, minHeight: 40 }
const small: CSSProperties = { ...input, minHeight: 36, width: 110 }

// Números em pt-BR: aceita vírgula. Vazio ou inválido vira NaN.
const parseNum = (s: string) => (s.trim() === '' ? NaN : Number(s.trim().replace(/\./g, '').replace(',', '.')))
const parseDec = (s: string) => (s.trim() === '' ? NaN : Number(s.trim().replace(',', '.')))
const showNum = (n: number) => (n === 0 ? '' : String(n).replace('.', ','))

function Section({ id, title, hint, children, action }: { id: string; title: string; hint?: string; children: ReactNode; action?: ReactNode }) {
  return (
    <section id={id} aria-labelledby={`${id}-t`} style={panel}>
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12 }}>
        <div>
          <h2 id={`${id}-t`} style={h2}>{title}</h2>
          {hint && <p style={{ ...muted, margin: '4px 0 0' }}>{hint}</p>}
        </div>
        {action}
      </div>
      {children}
    </section>
  )
}

function SaveRow({ pending, error, saved, label = 'Salvar', onSave }: { pending: boolean; error: unknown; saved: boolean; label?: string; onSave: () => void }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
      <button type="button" style={btnPrimary} disabled={pending} onClick={onSave}>{pending ? 'Salvando…' : label}</button>
      {saved && !pending && !error && <span role="status" style={{ ...muted, color: 'var(--ok)' }}>Salvo.</span>}
      <ErrorLine error={error} />
    </div>
  )
}

// Salva só os campos pedidos, sempre com o mapa completo de regras (o
// daemon rejeitaria "rules": null e perderia regras ausentes).
function useSaveSettings(settings: Settings) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (p: Partial<Settings>) => put<Settings>('/api/settings', { ...p, rules: { ...settings.rules, ...(p.rules ?? {}) } }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings'] }),
  })
}

// ---- Agentes ----
function Agents() {
  return (
    <Section
      id="agentes"
      title="Agentes detectados"
      hint="Detecção, instalação de hooks e skills: disponível na próxima etapa."
      action={
        <div style={{ display: 'flex', gap: 8 }}>
          <button type="button" style={btn} disabled>Escanear de novo</button>
          <button type="button" style={btn} disabled>Instalar o que falta</button>
        </div>
      }
    >
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))', gap: 12 }}>
        {AGENT_IDS.map((a) => (
          <article key={a} style={{ ...panel, background: 'var(--bg)', padding: 14 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
              <h3 style={{ fontSize: 15 }}><AgentSwatch agent={a} /></h3>
              <span className="badge">disponível na próxima etapa</span>
            </div>
            <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 8 }}>
              {SETUP_CHECKS[a].map((c) => (
                <li key={c} style={{ display: 'flex', gap: 8, color: 'var(--text-2)' }}>
                  <span aria-hidden="true">–</span>
                  <span>{c} <span style={{ fontSize: 12 }}>(não verificado)</span></span>
                </li>
              ))}
            </ul>
          </article>
        ))}
      </div>
    </Section>
  )
}

// ---- Limites ----
interface LimitDraft { daily: string; card: string; warn: string; action: 'pause' | 'warn' }
const limitDraft = (l?: Limit): LimitDraft => ({
  daily: showNum(l?.daily_usd ?? 0), card: showNum(l?.card_usd ?? 0), warn: String(l?.warn_pct ?? 80), action: l?.action ?? 'warn',
})

function Limits({ limits }: { limits: Limit[] }) {
  const qc = useQueryClient()
  const [rows, setRows] = useState<Record<string, LimitDraft>>(() => Object.fromEntries(AGENT_IDS.map((a) => [a, limitDraft(limits.find((l) => l.agent === a))])))
  const save = useMutation({
    mutationFn: async () => {
      const body: Limit[] = []
      for (const a of AGENT_IDS) {
        const r = rows[a]
        const l: Limit = { agent: a, daily_usd: r.daily === '' ? 0 : parseDec(r.daily), card_usd: r.card === '' ? 0 : parseDec(r.card), warn_pct: parseNum(r.warn), action: r.action }
        if (![l.daily_usd, l.card_usd].every((v) => v >= 0) || !(l.warn_pct >= 0 && l.warn_pct <= 100)) {
          throw new Error(`Valores inválidos em ${agentInfo(a).name}: use números e aviso entre 0 e 100.`)
        }
        body.push(l)
      }
      for (const l of body) await put('/api/limits', l)
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ['limits'] }),
  })
  const set = (a: string, p: Partial<LimitDraft>) => setRows((r) => ({ ...r, [a]: { ...r[a], ...p } }))
  return (
    <Section id="limites" title="Limites de custo" hint="Por agente. O aviso aparece na Sala de controle; a ação dispara ao chegar em 100%. Vazio ou 0 = sem limite.">
      <div style={{ overflowX: 'auto' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
          <thead>
            <tr style={thead}>
              <th scope="col" style={th}>Agente</th>
              <th scope="col" style={th}>Diário (US$)</th>
              <th scope="col" style={th}>Por card (US$)</th>
              <th scope="col" style={th}>Avisar em (%)</th>
              <th scope="col" style={th}>Ao chegar em 100%</th>
            </tr>
          </thead>
          <tbody>
            {AGENT_IDS.map((a) => {
              const n = agentInfo(a).name
              const r = rows[a]
              return (
                <tr key={a} style={{ borderTop: '1px solid var(--border)' }}>
                  <th scope="row" style={{ ...td, textAlign: 'left', fontWeight: 400 }}><AgentSwatch agent={a} /></th>
                  <td style={td}><input style={small} inputMode="decimal" aria-label={`Limite diário de ${n}`} value={r.daily} onChange={(e) => set(a, { daily: e.target.value })} /></td>
                  <td style={td}><input style={small} inputMode="decimal" aria-label={`Limite por card de ${n}`} value={r.card} onChange={(e) => set(a, { card: e.target.value })} /></td>
                  <td style={td}><input style={{ ...small, width: 80 }} inputMode="numeric" aria-label={`Percentual de aviso de ${n}`} value={r.warn} onChange={(e) => set(a, { warn: e.target.value })} /></td>
                  <td style={td}>
                    <select style={{ ...input, minHeight: 36 }} aria-label={`Ação em 100% para ${n}`} value={r.action} onChange={(e) => set(a, { action: e.target.value as 'pause' | 'warn' })}>
                      <option value="pause">Pausar a sessão e mover o card para Handoff</option>
                      <option value="warn">Só avisar</option>
                    </select>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      <SaveRow pending={save.isPending} saved={save.isSuccess} error={save.error} label="Salvar limites" onSave={() => save.mutate()} />
    </Section>
  )
}

// ---- Criação de cards ----
function CardCreation({ settings }: { settings: Settings }) {
  const save = useSaveSettings(settings)
  const [on, setOn] = useState(settings.reminder_edit_threshold > 0)
  const [n, setN] = useState(String(settings.reminder_edit_threshold > 0 ? settings.reminder_edit_threshold : 3))
  const [bad, setBad] = useState<Error | null>(null)
  const submit = () => {
    const v = parseNum(n)
    if (on && !(Number.isInteger(v) && v >= 1)) return setBad(new Error('Informe um número inteiro de arquivos (1 ou mais).'))
    setBad(null)
    save.mutate({ reminder_edit_threshold: on ? v : 0 })
  }
  return (
    <Section id="cards" title="Criação de cards" hint="Toda sessão é registrada, com tokens e custo. Um card só nasce quando o trabalho precisa de histórico.">
      <label style={check}>
        <input type="checkbox" checked disabled />
        <span>Quando eu pedir<br /><span style={muted}>“crie um card para isso”, <span className="mono">nobre start</span> ou um ID com <span className="mono">nobre resume</span> (sempre ativo)</span></span>
      </label>
      <label style={check}>
        <input type="checkbox" disabled />
        <span>Quando o agente identificar que está começando algo que precisa de histórico <span className="badge">em breve</span><br /><span style={muted}>Tarefa de várias etapas ou que pode ser continuada por outro agente. Perguntas rápidas não viram card.</span></span>
      </label>
      <label style={check}>
        <input type="checkbox" checked={on} onChange={(e) => setOn(e.target.checked)} />
        <span>Se ele esquecer, lembrar<br /><span style={muted}>O hook avisa o agente quando uma sessão sem card editar o número de arquivos abaixo ou mais.</span></span>
      </label>
      {on && (
        <label style={{ ...field, marginLeft: 28 }}>Arquivos editados antes do lembrete
          <input style={small} inputMode="numeric" value={n} onChange={(e) => setN(e.target.value)} />
        </label>
      )}
      <label style={check}>
        <input type="checkbox" disabled />
        <span>Mostrar cards pausados no início da sessão <span className="badge">em breve</span><br /><span style={muted}>O agente recebe os cards pausados do mesmo repositório e branch e pode retomar um deles em vez de criar outro.</span></span>
      </label>
      <SaveRow pending={save.isPending} saved={save.isSuccess} error={bad ?? save.error} onSave={submit} />
    </Section>
  )
}

// ---- Handoff ----
function Handoff({ settings }: { settings: Settings }) {
  const save = useSaveSettings(settings)
  const [rules, setRules] = useState<Record<string, boolean>>({ ...settings.rules })
  const [ttl, setTtl] = useState(String(settings.where_stopped_ttl_min))
  const [action, setAction] = useState(settings.incomplete_action)
  const [mode, setMode] = useState(settings.launch_mode || 'terminal')
  const [bad, setBad] = useState<Error | null>(null)
  const count = RULES.filter(([k]) => rules[k]).length
  const submit = () => {
    const v = parseNum(ttl)
    if (!(Number.isInteger(v) && v >= 0)) return setBad(new Error('A validade do “Onde parei” precisa ser um número de minutos.'))
    setBad(null)
    save.mutate({ rules, where_stopped_ttl_min: v, incomplete_action: action, launch_mode: mode })
  }
  return (
    <Section id="handoff" title="Regra de “pronto para handoff”" hint="O mínimo para outro agente continuar sem perguntar nada. Vale para todos os projetos.">
      <div style={{ display: 'flex', flexDirection: 'column' }}>
        {RULES.map(([k, text]) => (
          <label key={k} style={{ ...check, alignItems: 'center' }}>
            <input type="checkbox" checked={!!rules[k]} onChange={(e) => setRules((r) => ({ ...r, [k]: e.target.checked }))} />
            {text}
          </label>
        ))}
      </div>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 16 }}>
        <label style={field}>“Onde parei” vale por (minutos)
          <input style={small} inputMode="numeric" value={ttl} onChange={(e) => setTtl(e.target.value)} />
        </label>
        <label style={field}>Se a sessão terminar sem cumprir a regra
          <select style={{ ...input, minHeight: 40 }} value={action} onChange={(e) => setAction(e.target.value)}>
            <option value="ask_agent">Pedir ao agente que complete antes de sair</option>
            <option value="mark">Marcar “handoff incompleto” e avisar</option>
          </select>
        </label>
        <label style={field}>Ao clicar em “Continuar com…”, abrir o agente em
          <select style={{ ...input, minHeight: 40 }} value={mode} onChange={(e) => setMode(e.target.value)}>
            <option value="terminal">Nova janela do terminal</option>
            <option value="tmux" disabled>Sessão tmux (em breve)</option>
            <option value="background" disabled>Segundo plano, com saída na interface (em breve)</option>
          </select>
        </label>
      </div>
      <p style={{ ...muted, margin: 0 }}>Hoje: {count} {count === 1 ? 'critério ativo' : 'critérios ativos'}. O board mostra o progresso de cada card como barrinhas.</p>
      <SaveRow pending={save.isPending} saved={save.isSuccess} error={bad ?? save.error} onSave={submit} />
    </Section>
  )
}

// ---- Projetos ----
function Projects({ projects }: { projects: Project[] }) {
  const qc = useQueryClient()
  const cards = useCards().data ?? []
  const [name, setName] = useState('')
  const [path, setPath] = useState('')
  const [adding, setAdding] = useState(false)
  const refresh = () => qc.invalidateQueries({ queryKey: ['projects'] })
  const toggle = useMutation({ mutationFn: (p: Project) => patch(`/api/projects/${p.id}`, { capture: !p.capture }), onSuccess: refresh })
  const add = useMutation({
    mutationFn: () => post('/api/projects', { name: name.trim(), path: path.trim() }),
    onSuccess: async () => { await refresh(); setName(''); setPath(''); setAdding(false) },
  })
  return (
    <Section
      id="projetos"
      title="Projetos"
      hint="Só sessões dentro destes repositórios viram cards."
      action={!adding && <button type="button" style={btn} onClick={() => { add.reset(); setAdding(true) }}>Adicionar repositório</button>}
    >
      {adding && (
        <form
          onSubmit={(e) => { e.preventDefault(); add.mutate() }}
          style={{ display: 'flex', flexWrap: 'wrap', gap: 12, alignItems: 'flex-end' }}
        >
          <label style={field}>Nome (opcional)
            <input style={input} value={name} onChange={(e) => setName(e.target.value)} />
          </label>
          <label style={{ ...field, flex: 1, minWidth: 260 }}>Caminho absoluto
            <input style={{ ...input, fontFamily: 'var(--mono)' }} placeholder="/home/você/workspace/projeto" value={path} onChange={(e) => setPath(e.target.value)} />
          </label>
          <button type="submit" style={btnPrimary} disabled={add.isPending || path.trim() === ''}>Adicionar</button>
          <button type="button" style={btn} onClick={() => setAdding(false)}>Cancelar</button>
          <div style={{ flexBasis: '100%' }}><ErrorLine error={add.error} /></div>
        </form>
      )}
      {projects.length === 0 && <p style={muted}>Nenhum repositório cadastrado.</p>}
      {projects.map((p) => {
        const n = cards.filter((c) => c.project_id === p.id).length
        return (
          <div key={p.id} style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 12, padding: '10px 0', borderTop: '1px solid var(--border)' }}>
            <div style={{ flex: 1, minWidth: 200, display: 'flex', flexDirection: 'column' }}>
              <strong>{p.name}</strong>
              <span className="mono" style={{ ...muted, fontSize: 12 }}>{p.path}</span>
            </div>
            <span style={muted}>{p.capture ? `${n} ${n === 1 ? 'card' : 'cards'}` : 'captura desligada'}</span>
            <label style={{ display: 'inline-flex', alignItems: 'center', gap: 8, minHeight: 40 }}>
              <input type="checkbox" checked={p.capture} disabled={toggle.isPending} onChange={() => toggle.mutate(p)} aria-label={`Capturar sessões de ${p.name}`} />
              Capturar sessões
            </label>
          </div>
        )
      })}
      <ErrorLine error={toggle.error} />
    </Section>
  )
}

// ---- Preços e moeda ----
interface PriceDraft { input: string; output: string; cw: string; cr: string }
// Cache: null (desconhecido) fica em branco; 0 (grátis) aparece como "0".
const showCache = (n: number | null | undefined) => (n == null ? '' : String(n).replace('.', ','))
const priceDraft = (p?: Price): PriceDraft => ({ input: showNum(p?.input_per_m ?? 0), output: showNum(p?.output_per_m ?? 0), cw: showCache(p?.cache_write_per_m), cr: showCache(p?.cache_read_per_m) })

function PriceRow({ model, price, unpriced }: { model: string; price?: Price; unpriced: boolean }) {
  const qc = useQueryClient()
  const [d, setD] = useState(() => priceDraft(price))
  const [bad, setBad] = useState<Error | null>(null)
  const refresh = () => qc.invalidateQueries({ queryKey: ['prices'] })
  const save = useMutation({
    mutationFn: (p: Price) => put('/api/prices', p),
    onSuccess: refresh,
  })
  const remove = useMutation({ mutationFn: () => del(`/api/prices?model=${encodeURIComponent(model)}`), onSuccess: refresh })
  const submit = () => {
    const f = (s: string) => (s === '' ? 0 : parseDec(s))
    // Cache em branco = desconhecido (null), nunca 0.
    const fc = (s: string) => (s.trim() === '' ? null : parseDec(s))
    const p: Price = { model, input_per_m: f(d.input), output_per_m: f(d.output), cache_write_per_m: fc(d.cw), cache_read_per_m: fc(d.cr) }
    if (!(p.input_per_m > 0 && p.output_per_m > 0)) {
      return setBad(new Error('Informe os preços de entrada e saída (maiores que zero)'))
    }
    if (![p.cache_write_per_m, p.cache_read_per_m].every((v) => v === null || v >= 0)) {
      return setBad(new Error(`Preço inválido para ${model}.`))
    }
    setBad(null)
    save.mutate(p)
  }
  const cell = (k: keyof PriceDraft, label: string, placeholder = 'US$/1M') => (
    <td style={td}><input style={small} inputMode="decimal" placeholder={placeholder} aria-label={`${label} de ${model}`} value={d[k]} onChange={(e) => setD({ ...d, [k]: e.target.value })} /></td>
  )
  return (
    <tr style={{ borderTop: '1px solid var(--border)', background: unpriced ? '#1E1A12' : undefined }}>
      <th scope="row" style={{ ...td, textAlign: 'left', fontWeight: 400 }}>
        <span className="mono">{model}</span>
        {unpriced && <div style={{ color: 'var(--warn)', fontSize: 12 }}>⚠ visto no uso, sem preço</div>}
      </th>
      {cell('input', 'Preço de entrada')}
      {cell('output', 'Preço de saída')}
      {cell('cw', 'Preço de cache (escrita)', 'desconhecido')}
      {cell('cr', 'Preço de cache (leitura)', 'desconhecido')}
      <td style={td}>
        <div style={{ display: 'flex', gap: 6 }}>
          <button type="button" style={btn} aria-label={`Salvar preço de ${model}`} disabled={save.isPending} onClick={submit}>Salvar</button>
          {price && <button type="button" style={btn} aria-label={`Remover preço de ${model}`} disabled={remove.isPending} onClick={() => remove.mutate()}>Remover</button>}
        </div>
        <ErrorLine error={bad ?? save.error ?? remove.error} />
      </td>
    </tr>
  )
}

function Prices({ settings }: { settings: Settings }) {
  const save = useSaveSettings(settings)
  const prices = usePrices().data ?? []
  const models = useTotals('model').data ?? []
  const [currency, setCurrency] = useState(settings.currency)
  const [rate, setRate] = useState(showNum(settings.brl_rate))
  const [bad, setBad] = useState<Error | null>(null)
  const [extra, setExtra] = useState<string[]>([])
  const [newModel, setNewModel] = useState('')
  const unpricedSet = new Set(models.filter((m) => m.unpriced).map((m) => m.key))
  const names = Array.from(new Set([...prices.map((p) => p.model), ...models.map((m) => m.key), ...extra].filter(Boolean)))
  names.sort((a, b) => Number(unpricedSet.has(b) && !prices.some((p) => p.model === b)) - Number(unpricedSet.has(a) && !prices.some((p) => p.model === a)) || a.localeCompare(b))
  const submit = () => {
    const r = rate === '' ? 0 : parseDec(rate)
    if (!(r >= 0)) return setBad(new Error('Cotação inválida.'))
    if (currency === 'BRL' && !(r > 0)) return setBad(new Error('Informe a cotação US$ → R$ para exibir em reais.'))
    setBad(null)
    save.mutate({ currency, brl_rate: r })
  }
  return (
    <Section id="precos" title="Preços e moeda" hint="Preço por milhão de tokens. A tabela começa vazia: preencha com os valores públicos de cada provider. Modelo sem preço aparece como “sem preço”, nunca como zero. Cache: em branco = desconhecido (uso com cache fica “sem preço”); 0 = grátis.">
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 16, alignItems: 'flex-end' }}>
        <label style={field}>Moeda de exibição
          <select style={{ ...input, minHeight: 40 }} value={currency} onChange={(e) => setCurrency(e.target.value)}>
            <option value="USD">US$ (dólar)</option>
            <option value="BRL">R$ (real)</option>
          </select>
        </label>
        <label style={field}>Cotação US$ → R$
          <input style={small} inputMode="decimal" value={rate} onChange={(e) => setRate(e.target.value)} />
        </label>
        <SaveRow pending={save.isPending} saved={save.isSuccess} error={bad ?? save.error} label="Salvar moeda" onSave={submit} />
      </div>
      <div style={{ overflowX: 'auto' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
          <thead>
            <tr style={thead}>
              <th scope="col" style={th}>Modelo</th>
              <th scope="col" style={th}>Entrada</th>
              <th scope="col" style={th}>Saída</th>
              <th scope="col" style={th}>Cache (escrita)</th>
              <th scope="col" style={th}>Cache (leitura)</th>
              <th scope="col" style={th}><span style={{ position: 'absolute', width: 1, height: 1, overflow: 'hidden', clip: 'rect(0 0 0 0)' }}>Ações</span></th>
            </tr>
          </thead>
          <tbody>
            {names.length === 0 && <tr><td colSpan={6} style={{ ...td, color: 'var(--text-2)' }}>Nenhum modelo visto no uso ainda. Adicione um abaixo.</td></tr>}
            {names.map((m) => {
              const price = prices.find((p) => p.model === m)
              return <PriceRow key={`${m}:${price ? 1 : 0}`} model={m} price={price} unpriced={!price && unpricedSet.has(m)} />
            })}
          </tbody>
        </table>
      </div>
      <form
        onSubmit={(e) => { e.preventDefault(); const m = newModel.trim(); if (m) { setExtra((x) => [...x, m]); setNewModel('') } }}
        style={{ display: 'flex', gap: 8, alignItems: 'flex-end', flexWrap: 'wrap' }}
      >
        <label style={field}>Adicionar modelo
          <input style={{ ...input, fontFamily: 'var(--mono)' }} value={newModel} onChange={(e) => setNewModel(e.target.value)} placeholder="nome-do-modelo" />
        </label>
        <button type="submit" style={btn} disabled={newModel.trim() === ''}>Adicionar à tabela</button>
      </form>
    </Section>
  )
}

export function Config() {
  const settings = useSettings()
  const limits = useLimits()
  const projects = useProjects()
  const failed = settings.isError || limits.isError || projects.isError
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20, maxWidth: 1180 }}>
      <header style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
        <div>
          <h1 style={{ fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em' }}>Configuração</h1>
          <p style={{ ...muted, margin: '4px 0 0' }}>Agentes, limites, regra de handoff, projetos e preços. Cada seção tem seu próprio botão de salvar.</p>
        </div>
        <nav aria-label="Seções" style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
          {NAV.map(([id, label]) => (
            <a key={id} href={`#${id}`} className="badge" style={{ minHeight: 36, textDecoration: 'none', color: 'var(--nav-text)' }}>{label}</a>
          ))}
        </nav>
      </header>
      {failed && <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>Não foi possível carregar as configurações.</p>}
      {!failed && (settings.isPending || limits.isPending || projects.isPending) && <p className="placeholder">Carregando…</p>}
      <Agents />
      {limits.data && <Limits limits={limits.data} />}
      {settings.data && <CardCreation settings={settings.data} />}
      {settings.data && <Handoff settings={settings.data} />}
      {projects.data && <Projects projects={projects.data} />}
      {settings.data && <Prices settings={settings.data} />}
    </div>
  )
}
