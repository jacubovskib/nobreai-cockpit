import { useState, type CSSProperties, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { del, get, patch, post, put } from '../api/client'
import { useAgents, useCards, useLimits, usePrices, useProjects, useSettings, useTotals } from '../api/queries'
import type { AgentDetection, InstallReport, Limit, Price, Project, Settings } from '../api/types'
import { agentInfo } from '../ui/agents'
import { AgentSwatch } from '../ui/AgentSwatch'
import { AddProject } from './config/AddProject'
import { isGroup } from '../app/project'
import { RtkCard } from './config/RtkCard'
import { ConfigLog } from './config/ConfigLog'
import { ErrorLine, btn, btnPrimary, h2, input, muted, panel } from './card/shared'

const RULES: [string, string][] = [
  ['goal', 'Objetivo e “pronto quando” preenchidos'],
  ['where_stopped', '“Onde parei” atualizado recentemente'],
  ['essential_file', 'Pelo menos 1 arquivo essencial com motivo'],
  ['todo_open', 'Lista “Falta” não está vazia'],
  ['how_to_test', '“Como testar” preenchido'],
  ['no_open_questions', 'Nenhuma pergunta em aberto sem resposta'],
]
const NAV = [
  ['agentes', 'Agentes e setup'], ['limites', 'Limites de custo'], ['cards', 'Criação de cards'],
  ['handoff', 'Pronto para handoff'], ['contexto', 'Monitor de contexto'], ['projetos', 'Projetos'], ['precos', 'Preços e moeda'], ['historico', 'Histórico'],
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

// Texto das sessões abertas que vão receber o aviso da mudança.
export function notifiedText(n: number): string {
  return `${n} ${n === 1 ? 'sessão aberta vai' : 'sessões abertas vão'} receber o aviso no próximo passo.`
}

function SaveRow({ pending, error, saved, label = 'Salvar', onSave, notified }: { pending: boolean; error: unknown; saved: boolean; label?: string; onSave: () => void; notified?: number }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
      <button type="button" style={btnPrimary} disabled={pending} onClick={onSave}>{pending ? 'Salvando…' : label}</button>
      {saved && !pending && !error && <span role="status" style={{ ...muted, color: 'var(--ok)' }}>Salvo.</span>}
      {saved && !pending && !error && notified != null && notified > 0 && <span style={muted}>{notifiedText(notified)}</span>}
      <ErrorLine error={error} />
    </div>
  )
}

// Salva só os campos pedidos, sempre com o mapa completo de regras (o
// daemon rejeitaria "rules": null e perderia regras ausentes).
function useSaveSettings(settings: Settings) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (p: Partial<Settings>) => put<Settings & { notified_sessions?: number }>('/api/settings', { ...p, rules: { ...settings.rules, ...(p.rules ?? {}) } }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings'] }),
  })
}

// ---- Agentes ----
function InstallResult({ rep }: { rep?: InstallReport }) {
  if (!rep) return null
  return (
    <div role="status" style={{ ...muted, fontSize: 13 }}>
      {rep.messages.map((m) => <div key={m}>{m}</div>)}
      {rep.backup && <div>Backup: <code>{rep.backup}</code></div>}
    </div>
  )
}

function AgentCard({ info }: { info: AgentDetection }) {
  const qc = useQueryClient()
  const refresh = () => qc.invalidateQueries({ queryKey: ['agents'] })
  const install = useMutation({
    mutationFn: () => post<InstallReport>(`/api/agents/${info.agent}/install`),
    onSuccess: refresh,
  })
  // A status line é um comando do usuário: só muda com pedido explícito.
  const statusLine = useMutation({
    mutationFn: (action: 'install' | 'uninstall') => post<InstallReport>(`/api/agents/${info.agent}/statusline`, { action }),
    onSuccess: refresh,
  })
  const d = info.detection
  const { installable, status_line: hasStatusLine } = info
  const slIntegrated = d.checks.find((c) => c.key === 'statusline')?.ok === true
  return (
    <article aria-label={agentInfo(info.agent).name} style={{ ...panel, background: 'var(--bg)', padding: 14 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
        <h3 style={{ fontSize: 15 }}><AgentSwatch agent={info.agent} /></h3>
        <span className="badge">{installable ? (d.found ? 'detectado' : 'não encontrado') : 'em breve'}</span>
      </div>
      {installable && d.found && <p style={{ ...muted, margin: 0 }}>{d.path}{d.version ? ` · ${d.version}` : ''}</p>}
      <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 8 }}>
        {d.checks.map((c) => (
          <li key={c.key} style={{ display: 'flex', gap: 8, color: c.ok ? 'var(--text)' : 'var(--text-2)' }}>
            <span role="img" aria-label={c.ok ? 'ok' : 'pendente'} style={{ color: c.ok ? 'var(--ok)' : 'var(--text-2)' }}>{c.ok ? '✓' : '–'}</span>
            <span>
              {c.label}
              {c.detail && <><br /><span style={{ ...muted, fontSize: 12, wordBreak: 'break-all' }}>{c.detail}</span></>}
            </span>
          </li>
        ))}
      </ul>
      {installable && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          <button type="button" style={btnPrimary} disabled={install.isPending} onClick={() => install.mutate()}>
            {install.isPending ? 'Instalando…' : 'Instalar o que falta'}
          </button>
          <InstallResult rep={install.data} />
          <ErrorLine error={install.error} />
          {hasStatusLine && (
            <>
              <p style={{ ...muted, margin: 0, fontSize: 12 }}>
                Status line: o Cockpit entra no comando da sua status line para ler os limites do plano; a saída continua a mesma.
              </p>
              <button type="button" style={btn} disabled={statusLine.isPending}
                onClick={() => statusLine.mutate(slIntegrated ? 'uninstall' : 'install')}>
                {statusLine.isPending ? 'Aplicando…' : slIntegrated ? 'Remover integração' : 'Integrar status line'}
              </button>
              <InstallResult rep={statusLine.data} />
              <ErrorLine error={statusLine.error} />
            </>
          )}
        </div>
      )}
    </article>
  )
}

function PlanWarn({ settings }: { settings: Settings }) {
  const save = useSaveSettings(settings)
  const [v, setV] = useState(String(settings.plan_warn_pct ?? 80))
  const [bad, setBad] = useState<Error | null>(null)
  const submit = () => {
    const n = parseNum(v)
    if (!(Number.isInteger(n) && n >= 1 && n <= 100)) return setBad(new Error('Informe um percentual inteiro de 1 a 100.'))
    setBad(null)
    save.mutate({ plan_warn_pct: n })
  }
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 12, alignItems: 'flex-end' }}>
      <label style={field}>Avisar quando o limite do plano passar de (%)
        <input style={{ ...small, width: 80 }} inputMode="numeric" value={v} onChange={(e) => setV(e.target.value)} />
      </label>
      <SaveRow pending={save.isPending} saved={save.isSuccess} error={bad ?? save.error} label="Salvar aviso do plano" onSave={submit} />
    </div>
  )
}

function Agents({ settings }: { settings?: Settings }) {
  const agents = useAgents()
  return (
    <Section
      id="agentes"
      title="Agentes detectados"
      hint="Instala os hooks e a skill dos agentes neste computador; a status line, quando o agente tem uma, só é integrada quando você pedir."
      action={<button type="button" style={btn} disabled={agents.isFetching} onClick={() => agents.refetch()}>Escanear de novo</button>}
    >
      {agents.isError && <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>Não foi possível detectar os agentes.</p>}
      {agents.isPending && <p className="placeholder">Detectando…</p>}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))', gap: 12 }}>
        {agents.data?.map((a) => <AgentCard key={a.agent} info={a} />)}
        <RtkCard />
      </div>
      {settings && <PlanWarn settings={settings} />}
    </Section>
  )
}

// ---- Limites ----
interface LimitDraft { daily: string; card: string; warn: string; action: 'pause' | 'warn' }
const limitDraft = (l?: Limit): LimitDraft => ({
  daily: showNum(l?.daily_usd ?? 0), card: showNum(l?.card_usd ?? 0), warn: String(l?.warn_pct ?? 80), action: l?.action ?? 'warn',
})

function Limits({ limits, agents: AGENT_IDS }: { limits: Limit[]; agents: string[] }) {
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
                      <option value="pause">Bloquear o agente e mover o card para Pausado</option>
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
  const [creates, setCreates] = useState(settings.agent_creates_cards !== false)
  const [inject, setInject] = useState(settings.inject_paused_cards !== false)
  const [mem, setMem] = useState(String(settings.memory_budget_tokens ?? 1500))
  const [patterns, setPatterns] = useState((settings.plan_patterns ?? []).join('\n'))
  const [bad, setBad] = useState<Error | null>(null)
  const submit = () => {
    const v = parseNum(n)
    if (on && !(Number.isInteger(v) && v >= 1)) return setBad(new Error('Informe um número inteiro de arquivos (1 ou mais).'))
    const m = parseNum(mem)
    if (!(Number.isInteger(m) && m >= 0 && m <= 2000)) return setBad(new Error('O limite das memórias deve ficar entre 0 e 2000 tokens.'))
    setBad(null)
    save.mutate({ reminder_edit_threshold: on ? v : 0, agent_creates_cards: creates, inject_paused_cards: inject, memory_budget_tokens: m, plan_patterns: patterns.split('\n').map((l) => l.trim()).filter(Boolean) })
  }
  return (
    <Section id="cards" title="Criação de cards" hint="Toda sessão é registrada, com tokens e custo. Um card só nasce quando o trabalho precisa de histórico.">
      <label style={check}>
        <input type="checkbox" checked disabled />
        <span>Quando eu pedir<br /><span style={muted}>“crie um card para isso”, <span className="mono">nobre start</span> ou um ID com <span className="mono">nobre resume</span> (sempre ativo)</span></span>
      </label>
      <label style={check}>
        <input type="checkbox" checked={creates} onChange={(e) => setCreates(e.target.checked)} />
        <span>Quando o agente identificar que está começando algo que precisa de histórico<br /><span style={muted}>Tarefa de várias etapas ou que pode ser continuada por outro agente. Perguntas rápidas não viram card.</span></span>
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
        <input type="checkbox" checked={inject} onChange={(e) => setInject(e.target.checked)} />
        <span>Mostrar cards pausados no início da sessão<br /><span style={muted}>O agente recebe os cards pausados do mesmo repositório e branch e pode retomar um deles em vez de criar outro.</span></span>
      </label>
      <label style={field}>Limite das memórias por sessão (tokens)
        <input style={small} inputMode="numeric" value={mem} onChange={(e) => setMem(e.target.value)} />
        <span style={muted}>0 desliga; máximo 2000</span>
      </label>
      <div style={field}>
        <label htmlFor="plan-patterns">Arquivos de plano</label>
        <textarea id="plan-patterns" aria-describedby="plan-patterns-hint" style={{ ...small, width: '100%', minHeight: 72, padding: 8, fontFamily: 'var(--mono, monospace)' }} value={patterns} onChange={(e) => setPatterns(e.target.value)} />
        <span id="plan-patterns-hint" style={muted}>Um padrão por linha, relativo ao projeto. Vazio desliga a cobrança de plano.</span>
      </div>
      <SaveRow pending={save.isPending} saved={save.isSuccess} error={bad ?? save.error} onSave={submit} notified={save.data?.notified_sessions} />
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
  const [grace, setGrace] = useState(String(settings.grace_min ?? 15))
  const [term, setTerm] = useState((settings.terminal_cmd ?? []).join(' '))
  const [bad, setBad] = useState<Error | null>(null)
  const count = RULES.filter(([k]) => rules[k]).length
  const submit = () => {
    const v = parseNum(ttl)
    if (!(Number.isInteger(v) && v >= 0)) return setBad(new Error('A validade do “Onde parei” precisa ser um número de minutos.'))
    const g = parseNum(grace)
    if (!(Number.isInteger(g) && g >= 0)) return setBad(new Error('A carência precisa ser um número inteiro de minutos (0 ou mais).'))
    setBad(null)
    save.mutate({
      rules, where_stopped_ttl_min: v, incomplete_action: action, launch_mode: mode, grace_min: g,
      ...(mode === 'terminal' ? { terminal_cmd: term.split(/\s+/).filter(Boolean) } : {}),
    })
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
            <option value="mark">Marcar o card como “handoff incompleto” e avisar na Sala de controle</option>
          </select>
        </label>
        <label style={field}>Ao clicar em “Continuar com…”, abrir o agente em
          <select style={{ ...input, minHeight: 40 }} value={mode} onChange={(e) => setMode(e.target.value)}>
            <option value="terminal">Nova janela do terminal</option>
            <option value="tmux" disabled>Sessão tmux (em breve)</option>
            <option value="background">Segundo plano, com saída na interface</option>
          </select>
        </label>
        <label style={field}>Carência antes de encerrar uma sessão cujo processo sumiu (minutos)
          <input style={small} inputMode="numeric" value={grace} onChange={(e) => setGrace(e.target.value)} />
        </label>
        {mode === 'terminal' && (
          <label style={field}>Comando do terminal (separado por espaços)
            <input style={{ ...input, minHeight: 40, width: 280 }} className="mono" value={term} onChange={(e) => setTerm(e.target.value)} />
          </label>
        )}
      </div>
      <p style={{ ...muted, margin: 0 }}>Hoje: {count} {count === 1 ? 'critério ativo' : 'critérios ativos'}. O board mostra o progresso de cada card como barrinhas.</p>
      <SaveRow pending={save.isPending} saved={save.isSuccess} error={bad ?? save.error} onSave={submit} />
    </Section>
  )
}

// ---- Projetos ----
interface Suggestion { path: string; rel: string; name: string; source: string }
interface Moved { moved?: { sessions: number; cards: number } }

// Pais válidos de um projeto solto: grupos e monorepos cuja pasta contém a dele.
function validParents(p: Project, projects: Project[]): Project[] {
  return projects.filter((q) => {
    if (q.id === p.id || q.parent_id != null) return false
    if (isGroup(q) && q.path === '') return true
    return p.path.startsWith(q.path.replace(/\/+$/, '') + '/')
  })
}

function Suggestions({ project }: { project: Project }) {
  const qc = useQueryClient()
  const list = useQuery({ queryKey: ['suggestions', project.id], queryFn: () => get<Suggestion[]>(`/api/projects/${project.id}/suggestions`) })
  const [off, setOff] = useState<string[]>([])
  const items = list.data ?? []
  const chosen = items.filter((s) => !off.includes(s.rel))
  const done = () => Promise.all([qc.invalidateQueries({ queryKey: ['projects'] }), qc.invalidateQueries({ queryKey: ['suggestions', project.id] })])
  const addAll = useMutation({
    mutationFn: async () => { for (const s of chosen) await post('/api/projects', { name: s.name, path: s.path, parent_id: project.id }) },
    onSettled: done,
  })
  const dismiss = useMutation({
    mutationFn: () => post(`/api/projects/${project.id}/suggestions/dismiss`, { rels: chosen.map((s) => s.rel) }),
    onSuccess: done,
  })
  if (items.length === 0) return null
  return (
    <div style={{ flexBasis: '100%', display: 'flex', flexDirection: 'column', gap: 6, padding: '8px 12px', borderRadius: 8, border: '1px solid var(--border)' }}>
      <strong>{items.length} {items.length === 1 ? 'sub-projeto sugerido' : 'sub-projetos sugeridos'}</strong>
      {items.map((s) => (
        <label key={s.rel} style={{ ...check, minHeight: 32, alignItems: 'center' }}>
          <input type="checkbox" checked={!off.includes(s.rel)} onChange={(e) => setOff((o) => (e.target.checked ? o.filter((r) => r !== s.rel) : [...o, s.rel]))} />
          <span>{s.name} <span className="mono" style={{ ...muted, fontSize: 12 }}>{s.rel}</span></span>
        </label>
      ))}
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        <button type="button" style={btnPrimary} disabled={chosen.length === 0 || addAll.isPending} onClick={() => addAll.mutate()}>Adicionar selecionados</button>
        <button type="button" style={btn} disabled={chosen.length === 0 || dismiss.isPending} onClick={() => dismiss.mutate()}>Não sugerir estes</button>
      </div>
      <ErrorLine error={addAll.error ?? dismiss.error} />
    </div>
  )
}

function ProjectRow({ p, projects, nCards, child, report, setReport, onRescan }: { p: Project; projects: Project[]; nCards: number; child: boolean; report: string; setReport: (id: number, text: string) => void; onRescan?: (path: string) => void }) {
  const qc = useQueryClient()
  const parent = p.parent_id == null ? undefined : projects.find((x) => x.id === p.parent_id)
  const hasKids = projects.some((x) => x.parent_id === p.id)
  const loose = !child && !hasKids && p.path !== '' && !isGroup(p)
  const options = loose ? validParents(p, projects) : []
  const [target, setTarget] = useState<number | null>(null)
  const refresh = () => Promise.all([qc.invalidateQueries({ queryKey: ['projects'] }), qc.invalidateQueries({ queryKey: ['cards'] })])
  const toggle = useMutation({ mutationFn: () => patch<{ notified_sessions?: number }>(`/api/projects/${p.id}`, { capture: !p.capture }), onSuccess: refresh })
  const move = useMutation({
    mutationFn: (parentId: number | null) => patch<Moved>(`/api/projects/${p.id}`, { parent_id: parentId }),
    onSuccess: async (r) => {
      setTarget(null)
      setReport(p.id, r.moved ? `${r.moved.sessions} ${r.moved.sessions === 1 ? 'sessão' : 'sessões'} e ${r.moved.cards} ${r.moved.cards === 1 ? 'card' : 'cards'} passaram para ${p.name}.` : '')
      await refresh()
    },
  })
  const dest = target == null ? undefined : projects.find((x) => x.id === target)
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 12, padding: '10px 0', paddingLeft: child ? 28 : 0, borderTop: '1px solid var(--border)' }}>
      <div style={{ flex: 1, minWidth: 200, display: 'flex', flexDirection: 'column' }}>
        <strong>{p.name}{isGroup(p) && <span className="badge" style={{ marginLeft: 8 }}>grupo</span>}</strong>
        {p.path !== '' && <span className="mono" style={{ ...muted, fontSize: 12 }}>{p.path}</span>}
      </div>
      <span style={muted}>{child ? (parent?.capture ? `${nCards} ${nCards === 1 ? 'card' : 'cards'}` : 'captura desligada') : p.capture ? `${nCards} ${nCards === 1 ? 'card' : 'cards'}` : 'captura desligada'}</span>
      {!child && (
        <label style={{ display: 'inline-flex', alignItems: 'center', gap: 8, minHeight: 40 }}>
          <input type="checkbox" checked={p.capture} disabled={toggle.isPending} onChange={() => toggle.mutate()} aria-label={`Capturar sessões de ${p.name}`} />
          Capturar sessões
        </label>
      )}
      {!child && toggle.isSuccess && (toggle.data?.notified_sessions ?? 0) > 0 && <span role="status" style={muted}>{notifiedText(toggle.data!.notified_sessions!)}</span>}
      {loose && options.length > 0 && target == null && (
        <select style={{ ...input, minHeight: 36 }} aria-label={`Mover ${p.name} para…`} value="" onChange={(e) => { setReport(p.id, ''); setTarget(Number(e.target.value)) }}>
          <option value="" disabled>Mover para…</option>
          {options.map((o) => <option key={o.id} value={o.id}>{o.name}</option>)}
        </select>
      )}
      {!child && p.path !== '' && onRescan && <button type="button" style={btn} aria-label={`Varrer de novo ${p.name}`} onClick={() => onRescan(p.path)}>Varrer de novo</button>}
      {child && <button type="button" style={btn} disabled={move.isPending} onClick={() => { setReport(p.id, ''); move.mutate(null) }}>Desligar do projeto principal</button>}
      {dest && (
        <div style={{ flexBasis: '100%', display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'center' }}>
          {dest.capture !== p.capture && <span style={muted}>A captura de {p.name} passa a seguir a de {dest.name} ({dest.capture ? 'ligada' : 'desligada'}).</span>}
          <button type="button" style={btnPrimary} disabled={move.isPending} onClick={() => move.mutate(dest.id)}>Mover para {dest.name}</button>
          <button type="button" style={btn} onClick={() => setTarget(null)}>Cancelar</button>
        </div>
      )}
      {report && <span role="status" style={{ ...muted, flexBasis: '100%', color: 'var(--ok)' }}>{report}</span>}
      <div style={{ flexBasis: '100%' }}><ErrorLine error={toggle.error ?? move.error} /></div>
      {!child && p.path !== '' && <Suggestions project={p} />}
    </div>
  )
}

function Projects({ projects }: { projects: Project[] }) {
  const qc = useQueryClient()
  const cards = useCards().data ?? []
  // adding: painel Adicionar projeto; path preenchido = Varrer de novo (abre no resultado).
  const [adding, setAdding] = useState<{ path?: string; n: number } | null>(null)
  // O relatório de "Mover para…" fica aqui: a linha remonta quando muda de pai.
  const [reports, setReports] = useState<Record<number, string>>({})
  const setReport = (id: number, text: string) => setReports((r) => ({ ...r, [id]: text }))
  const [groupOpen, setGroupOpen] = useState(false)
  const [group, setGroup] = useState('')
  const refresh = () => qc.invalidateQueries({ queryKey: ['projects'] })
  const addGroup = useMutation({
    mutationFn: () => post('/api/projects', { name: group.trim() }),
    onSuccess: async () => { await refresh(); setGroup(''); setGroupOpen(false) },
  })
  const roots = projects.filter((p) => p.parent_id == null)
  return (
    <Section
      id="projetos"
      title="Projetos"
      hint="Só sessões dentro destes repositórios viram cards. Um monorepo pode ter sub-projetos; a captura segue a do projeto principal."
      action={!adding && !groupOpen && (
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          <button type="button" style={btn} onClick={() => { addGroup.reset(); setGroupOpen(true) }}>Novo grupo</button>
          <button type="button" style={btn} onClick={() => setAdding({ n: Date.now() })}>Adicionar projeto</button>
        </div>
      )}
    >
      {groupOpen && (
        <form onSubmit={(e) => { e.preventDefault(); addGroup.mutate() }} style={{ display: 'flex', flexWrap: 'wrap', gap: 12, alignItems: 'flex-end' }}>
          <label style={field}>Nome do grupo
            <input style={input} value={group} onChange={(e) => setGroup(e.target.value)} />
          </label>
          <button type="submit" style={btnPrimary} disabled={addGroup.isPending || group.trim() === ''}>Criar grupo</button>
          <button type="button" style={btn} onClick={() => setGroupOpen(false)}>Cancelar</button>
          <div style={{ flexBasis: '100%' }}><ErrorLine error={addGroup.error} /></div>
        </form>
      )}
      {adding && <AddProject key={adding.n} initialPath={adding.path} onClose={() => setAdding(null)} />}
      {projects.length === 0 && <p style={muted}>Nenhum repositório cadastrado.</p>}
      {roots.map((p) => (
        <div key={p.id}>
          <ProjectRow p={p} projects={projects} nCards={cards.filter((c) => c.project_id === p.id).length} child={false} report={reports[p.id] ?? ''} setReport={setReport} onRescan={(path) => setAdding({ path, n: Date.now() })} />
          {projects.filter((k) => k.parent_id === p.id).map((k) => (
            <ProjectRow key={k.id} p={k} projects={projects} nCards={cards.filter((c) => c.project_id === k.id).length} child report={reports[k.id] ?? ''} setReport={setReport} />
          ))}
        </div>
      ))}
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

// ---- Monitor de contexto ----
// Mesmas regras do servidor (validateContextSettings).
function ContextMonitor({ settings }: { settings: Settings }) {
  const save = useSaveSettings(settings)
  const [bands, setBands] = useState<string[]>(() => (settings.context_warn_pcts ?? [60, 80]).map(String))
  const [big, setBig] = useState(String(settings.big_output_tokens ?? 15000))
  const [max, setMax] = useState(String(settings.big_output_max ?? 3))
  const [warn, setWarn] = useState(settings.agent_context_warnings ?? true)
  const [bad, setBad] = useState<Error | null>(null)
  const submit = () => {
    const pcts = bands.map(parseNum)
    const bg = parseNum(big)
    const mx = parseNum(max)
    const ok = (n: number) => Number.isInteger(n)
    if (!pcts.every((n, i) => ok(n) && n >= 1 && n <= 99 && (i === 0 || n > pcts[i - 1]))) return setBad(new Error('As faixas devem ficar entre 1 e 99, em ordem crescente.'))
    if (!ok(bg) || bg < 1000) return setBad(new Error('A saída grande deve ter pelo menos 1000 tokens.'))
    if (!ok(mx) || mx < 0 || mx > 20) return setBad(new Error('O máximo de avisos deve ficar entre 0 e 20.'))
    setBad(null)
    save.mutate({ context_warn_pcts: pcts, big_output_tokens: bg, big_output_max: mx, agent_context_warnings: warn })
  }
  return (
    <Section id="contexto" title="Monitor de contexto" hint="Avisos ao agente quando o contexto da sessão enche ou uma saída de ferramenta fica grande. Nada aqui chama modelo.">
      <label style={check}>
        <input type="checkbox" checked={warn} onChange={(e) => setWarn(e.target.checked)} />
        <span>Avisar o agente</span>
      </label>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        {bands.length === 0 && <p style={{ ...muted, margin: 0 }}>Sem avisos de faixa: o agente só é avisado de saídas grandes.</p>}
        {bands.map((v, i) => (
          <div key={i} style={{ display: 'flex', gap: 8, alignItems: 'flex-end', flexWrap: 'wrap' }}>
            <label style={field}>Faixa {i + 1} (% da janela)
              <input style={small} inputMode="numeric" value={v} onChange={(e) => setBands((b) => b.map((x, j) => (j === i ? e.target.value : x)))} />
            </label>
            <button type="button" style={btn} aria-label={`Remover faixa ${i + 1}`} onClick={() => setBands((b) => b.filter((_, j) => j !== i))}>Remover</button>
          </div>
        ))}
        <div><button type="button" style={btn} onClick={() => setBands((b) => [...b, ''])}>Adicionar faixa</button></div>
      </div>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 16, alignItems: 'flex-end' }}>
        <label style={field}>Saída grande (tokens)
          <input style={small} inputMode="numeric" value={big} onChange={(e) => setBig(e.target.value)} />
        </label>
        <label style={field}>Máximo de avisos de saída por sessão
          <input style={small} inputMode="numeric" value={max} onChange={(e) => setMax(e.target.value)} />
        </label>
      </div>
      <SaveRow pending={save.isPending} saved={save.isSuccess} error={bad ?? save.error} label="Salvar" onSave={submit} />
    </Section>
  )
}

interface WindowRow { model: string; tokens: string }

// Janela de contexto por modelo: o que não está na tabela usa 200 mil (e [1m] no nome, 1 milhão).
function ModelWindows({ settings }: { settings: Settings }) {
  const save = useSaveSettings(settings)
  const [rows, setRows] = useState<WindowRow[]>(() =>
    Object.entries(settings.model_windows ?? {}).sort(([a], [b]) => a.localeCompare(b)).map(([model, n]) => ({ model, tokens: String(n) })),
  )
  const [bad, setBad] = useState<Error | null>(null)
  const edit = (i: number, patch: Partial<WindowRow>) => setRows((r) => r.map((x, j) => (j === i ? { ...x, ...patch } : x)))
  const submit = () => {
    const map: Record<string, number> = {}
    for (const r of rows) {
      const m = r.model.trim()
      const n = parseNum(r.tokens)
      if (m === '') return setBad(new Error('Informe o nome do modelo em todas as linhas.'))
      if (m in map) return setBad(new Error(`O modelo ${m} aparece mais de uma vez.`))
      if (!Number.isInteger(n) || n < 1000) return setBad(new Error(`A janela de ${m} deve ser de pelo menos 1000 tokens.`))
      map[m] = n
    }
    setBad(null)
    save.mutate({ model_windows: map })
  }
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      <h3 style={h2}>Janela de contexto por modelo</h3>
      <p style={{ ...muted, margin: 0 }}>Tamanho da janela em tokens. Modelo fora da tabela usa 200.000 (ou 1.000.000 com [1m] no nome) e aparece como “janela estimada”.</p>
      {rows.map((r, i) => (
        <div key={i} style={{ display: 'flex', gap: 8, alignItems: 'flex-end', flexWrap: 'wrap' }}>
          <label style={field}>Modelo
            <input style={{ ...input, fontFamily: 'var(--mono)' }} value={r.model} onChange={(e) => edit(i, { model: e.target.value })} />
          </label>
          <label style={field}>Janela (tokens)
            <input style={small} inputMode="numeric" value={r.tokens} onChange={(e) => edit(i, { tokens: e.target.value })} />
          </label>
          <button type="button" style={btn} aria-label={`Remover janela de ${r.model || 'modelo novo'}`} onClick={() => setRows((x) => x.filter((_, j) => j !== i))}>Remover</button>
        </div>
      ))}
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        <button type="button" style={btn} onClick={() => setRows((x) => [...x, { model: '', tokens: '200000' }])}>Adicionar janela</button>
      </div>
      <SaveRow pending={save.isPending} saved={save.isSuccess} error={bad ?? save.error} label="Salvar janelas" onSave={submit} />
    </div>
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
      <ModelWindows settings={settings} />
    </Section>
  )
}

export function Config() {
  const settings = useSettings()
  const limits = useLimits()
  const agentList = useAgents()
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
      <Agents settings={settings.data} />
      {limits.data && agentList.data && <Limits limits={limits.data} agents={agentList.data.filter((a) => !a.spends_credits).map((a) => a.agent)} />}
      {settings.data && <CardCreation settings={settings.data} />}
      {settings.data && <Handoff settings={settings.data} />}
      {settings.data && <ContextMonitor settings={settings.data} />}
      {projects.data && <Projects projects={projects.data} />}
      {settings.data && <Prices settings={settings.data} />}
      <ConfigLog />
    </div>
  )
}
