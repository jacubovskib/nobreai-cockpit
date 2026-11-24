import type { CardSummary, Project, SessionWithUsage, Total } from '../api/types'
import { signalText } from './board'

export interface ProjectRow {
  project: Project | null
  costUsd: number
  // credits: gasto em créditos (Kiro), à parte do costUsd.
  credits: number
  unpriced: boolean
  current: CardSummary | null
  stale: { count: number; oldest: string | null }
  awaiting: { count: number; oldest: string | null }
  lastActivity: string | null
  // Só em um pai: as linhas dos apps e o que caiu no próprio pai ("geral").
  // costUsd do pai já é o total da família.
  children: ProjectRow[]
  general: ProjectRow | null
}

const ms = (iso: string | null) => {
  const t = iso ? Date.parse(iso) : NaN
  return Number.isNaN(t) ? 0 : t
}

// activity: o maior entre last_activity_at (sessões) e updated_at (coluna / "Onde parei").
export function activity(c: CardSummary): string | null {
  return ms(c.last_activity_at) >= ms(c.updated_at) ? c.last_activity_at ?? c.updated_at ?? null : c.updated_at
}

function signalSummary(cards: CardSummary[], signal: CardSummary['signal']) {
  const hit = cards.filter((c) => c.signal === signal)
  let oldest: string | null = null
  for (const c of hit) if (c.signal_since && (oldest === null || ms(c.signal_since) < ms(oldest))) oldest = c.signal_since
  return { count: hit.length, oldest }
}

// sessionProject: o projeto de uma sessão é o do card dela; sem card, o da própria sessão.
// O daemon já resolve isso em effective_project_id.
export const sessionProject = (s: SessionWithUsage): number | null => s.effective_project_id ?? s.project_id

function row(project: Project | null, cards: CardSummary[], total: Total | undefined, sessions: SessionWithUsage[] = []): ProjectRow {
  let current: CardSummary | null = null
  let last: string | null = null
  for (const c of cards) {
    const a = activity(c)
    if (ms(a) > ms(last)) last = a
    if (['em_andamento', 'pausado', 'revisao'].includes(c.column) && (current === null || ms(a) > ms(activity(current)))) current = c
  }
  // Última atividade: também a das sessões do projeto (uma sessão sem card não aparece nos cards).
  for (const x of sessions) if (ms(x.last_event_at) > ms(last)) last = x.last_event_at
  return {
    project,
    costUsd: total?.cost_usd ?? 0,
    credits: total?.credits ?? 0,
    unpriced: total?.unpriced ?? false,
    current,
    stale: signalSummary(cards, 'parado'),
    awaiting: signalSummary(cards, 'aguardando_revisao'),
    lastActivity: last,
    children: [],
    general: null,
  }
}

// Soma em micro-dólares inteiros: a soma das linhas bate exatamente com o total.
const sumUsd = (xs: number[]) => xs.reduce((s, x) => s + Math.round(x * 1e6), 0) / 1e6

function familyRow(parent: Project, kids: Project[], cards: CardSummary[], byKey: Map<string, Total>, sessions: SessionWithUsage[]): ProjectRow {
  const of = (id: number) => sessions.filter((x) => sessionProject(x) === id)
  const general = row(parent, cards.filter((c) => c.project_id === parent.id), byKey.get(String(parent.id)), of(parent.id))
  const children = kids.map((k) => row(k, cards.filter((c) => c.project_id === k.id), byKey.get(String(k.id)), of(k.id)))
  const family = new Set([parent.id, ...kids.map((k) => k.id)])
  const all = row(parent, cards.filter((c) => c.project_id !== null && family.has(c.project_id)), undefined, sessions.filter((x) => { const id = sessionProject(x); return id !== null && family.has(id) }))
  const parts = [general, ...children]
  return {
    ...all,
    costUsd: sumUsd(parts.map((r) => r.costUsd)),
    credits: sumUsd(parts.map((r) => r.credits)),
    unpriced: parts.some((r) => r.unpriced),
    children,
    general,
  }
}

// buildRows: uma linha por projeto raiz (ordem de id), com os apps dentro do pai; "Sem projeto" só com custo > 0.
export function buildRows(projects: Project[], cards: CardSummary[], totals: Total[], sessions: SessionWithUsage[]): ProjectRow[] {
  const byKey = new Map(totals.map((t) => [t.key, t]))
  const ids = new Set(projects.map((p) => p.id))
  const rows = projects
    .filter((p) => p.parent_id == null || !ids.has(p.parent_id))
    .sort((a, b) => a.id - b.id)
    .map((p) => {
      const kids = projects.filter((k) => k.parent_id === p.id).sort((a, b) => a.name.localeCompare(b.name))
      return kids.length === 0
        ? row(p, cards.filter((c) => c.project_id === p.id), byKey.get(String(p.id)), sessions.filter((x) => sessionProject(x) === p.id))
        : familyRow(p, kids, cards, byKey, sessions)
    })
  const none = byKey.get('')
  if (none && (none.cost_usd > 0 || none.credits > 0)) rows.push(row(null, [], none))
  return rows
}

export type Group = 'concluidas' | 'andamento' | 'paradas' | 'backlog'
export const GROUPS: { id: Group; label: string }[] = [
  { id: 'concluidas', label: 'Concluídas' },
  { id: 'andamento', label: 'Em andamento' },
  { id: 'paradas', label: 'Paradas' },
  { id: 'backlog', label: 'Backlog' },
]

export function timeline(cards: CardSummary[]): { group: Group; card: CardSummary }[] {
  const of = (g: Group, list: CardSummary[], cmp: (a: CardSummary, b: CardSummary) => number) =>
    [...list].sort(cmp).map((card) => ({ group: g, card }))
  const done = cards.filter((c) => c.column === 'concluido')
  const stopped = cards.filter((c) => c.signal === 'parado')
  const going = cards.filter((c) => c.signal !== 'parado' && ['em_andamento', 'pausado', 'revisao'].includes(c.column))
  const backlog = cards.filter((c) => c.column === 'backlog')
  return [
    ...of('concluidas', done, (a, b) => ms(a.created_at) - ms(b.created_at)),
    ...of('andamento', going, (a, b) => ms(activity(b)) - ms(activity(a))),
    ...of('paradas', stopped, (a, b) => ms(a.signal_since) - ms(b.signal_since)),
    ...of('backlog', backlog, (a, b) => ms(a.created_at) - ms(b.created_at)),
  ]
}

// ddmm: "dd/mm" em pt-BR.
export function ddmm(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleDateString('pt-BR', { day: '2-digit', month: '2-digit' })
}

// contextLine: o que a entrega está fazendo agora, a partir do plano. Vazio sem plano.
export function contextLine(c: CardSummary): string {
  if (c.column === 'concluido') return c.branch_merged_at ? `merge ${ddmm(c.branch_merged_at)}` : ''
  const tasks = c.plan?.tasks ?? []
  if (tasks.length === 0) return ''
  const active = tasks.find((t) => t.status === 'em_andamento' || t.status === 'em_revisao')
  if (active) return `tarefa ${active.n}: ${active.title}${active.status === 'em_revisao' ? ' (em revisão)' : ''}`
  if (tasks.every((t) => t.status === 'a_fazer')) return 'plano escrito, não iniciado'
  if (tasks.every((t) => t.status === 'concluida')) return 'tarefas concluídas'
  const next = tasks.find((t) => t.status === 'a_fazer')
  return next ? `próxima: tarefa ${next.n}: ${next.title}` : ''
}

export function stateLabel(c: CardSummary): string {
  if (c.column === 'concluido') return 'concluída'
  if (c.signal === 'parado' || c.signal === 'aguardando_revisao') return signalText(c)
  if (c.column === 'backlog') return 'backlog'
  return 'em andamento'
}
