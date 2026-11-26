import type { Card, CardSummary, CardType, Column, Priority } from '../api/types'

export const COLUMNS: { id: Column; label: string; rule: string; dot: string }[] = [
  { id: 'backlog', label: 'Backlog', rule: 'Criado por você ou por um agente, sem sessão ainda', dot: '#6B7280' },
  { id: 'em_andamento', label: 'Em andamento', rule: 'Uma sessão viva é dona do card', dot: '#3987E5' },
  { id: 'pausado', label: 'Pausado / Handoff', rule: 'Sessão terminou sem concluir; qualquer agente pode retomar', dot: '#FAB219' },
  { id: 'revisao', label: 'Revisão', rule: 'O agente declarou pronto; aguarda você', dot: '#9085E9' },
  { id: 'concluido', label: 'Concluído', rule: 'Aprovado por você', dot: '#0CA30C' },
]

// groupByColumn devolve sempre as 5 colunas, na ordem fixa, mesmo vazias.
export function groupByColumn(cards: CardSummary[]): Record<Column, CardSummary[]> {
  const out = {} as Record<Column, CardSummary[]>
  for (const c of COLUMNS) out[c.id] = []
  for (const card of cards) out[card.column]?.push(card)
  return out
}

// minutesSince: minutos inteiros desde um instante RFC3339 (0 se vazio/futuro).
export function minutesSince(iso: string | null, now: number = Date.now()): number {
  if (!iso) return 0
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return 0
  return Math.max(0, Math.floor((now - t) / 60000))
}

export const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`

// signalText: "parado há 30 h" / "aguardando você há 3 dias" / "bloqueado por NAC-4". Abaixo
// de 48 h mostra horas; a partir daí, dias inteiros (arredondados para baixo).
// Sem sinal, vazio.
export function signalText(card: Pick<CardSummary, 'signal' | 'signal_since' | 'blocked_by'>, now: Date = new Date()): string {
  const { signal, signal_since: since } = card
  if (!signal || !since) return ''
  if (signal === 'bloqueado') return `bloqueado por ${card.blocked_by}`
  if (signal === 'pronto_para_retomar') return `pronto para retomar (${card.blocked_by} concluído)`
  const t = Date.parse(since)
  if (Number.isNaN(t)) return ''
  const hours = Math.max(0, Math.floor((now.getTime() - t) / 3600000))
  const label = signal === 'parado' ? 'parado' : signal === 'aguardando_voce' ? 'aguardando você' : 'aguardando revisão'
  return hours < 48 ? `${label} há ${hours} h` : `${label} há ${Math.floor(hours / 24)} dias`
}

// signalColor: parado = alerta, pronto para retomar = ok, demais = secundária.
export function signalColor(signal: CardSummary['signal']): string {
  return signal === 'parado' ? 'var(--warn)' : signal === 'pronto_para_retomar' ? 'var(--ok)' : 'var(--text-2)'
}

// cardMarks: marcas do card (limite de custo e handoff incompleto, este só em
// Pausado e até ficar pronto).
export function cardMarks(card: Pick<Card, 'column' | 'pause_reason' | 'handoff_incomplete'>, ready: boolean): string[] {
  const out: string[] = []
  if (card.pause_reason === 'limite de custo') out.push('pausado por limite de custo')
  if (card.handoff_incomplete && !ready && card.column === 'pausado') out.push('handoff incompleto')
  return out
}

export const TYPE_LABEL: Record<CardType, string> = { epico: 'Épico', historia: 'História', tarefa: 'Tarefa', bug: 'Bug' }
export const TYPE_ICON: Record<CardType, string> = { epico: '◆', historia: '■', tarefa: '●', bug: '▲' }
export const PRIORITY_LABEL: Record<Priority, string> = {
  mais_alta: 'Mais alta', alta: 'Alta', media: 'Média', baixa: 'Baixa', mais_baixa: 'Mais baixa',
}

// epic: chave do épico, 'none' (sem pai e não-épico) ou '' (todos).
// search: texto livre casado com chave e título (o payload do board não traz arquivos).
export interface BoardFilter { epic: string | 'none' | ''; type: CardType | ''; waiting: boolean; search?: string }

// fold: minúsculas e sem acentos, para a busca.
const fold = (s: string) => s.normalize('NFD').replace(/[\u0300-\u036f]/g, '').toLowerCase()

// matchesSearch: a busca casa com a chave ou o título; vazia casa tudo.
export function matchesSearch(card: Pick<CardSummary, 'key' | 'title'>, search: string): boolean {
  const term = fold(search.trim())
  return !term || fold(card.key).includes(term) || fold(card.title ?? '').includes(term)
}

// applyBoardFilter: épico mantém o épico e as filhas; 'none' = sem pai e não-épico.
export function applyBoardFilter(cards: CardSummary[], f: BoardFilter): CardSummary[] {
  return cards.filter((c) => {
    if (f.epic === 'none') { if (c.parent_key || c.type === 'epico') return false }
    else if (f.epic && c.key !== f.epic && c.parent_key !== f.epic) return false
    if (f.type && c.type !== f.type) return false
    if (f.waiting && !c.waiting_on_you) return false
    if (f.search && !matchesSearch(c, f.search)) return false
    return true
  })
}

// epicOptions: os épicos da lista e os citados por parent_key das filhas dela
// (o épico pode estar em outro projeto). `selected` não encontrado entra só com a chave.
export function epicOptions(cards: CardSummary[], selected = ''): { key: string; title: string }[] {
  const out = new Map<string, string>()
  for (const c of cards) if (c.type === 'epico') out.set(c.key, c.title)
  for (const c of cards) if (c.parent_key && !out.has(c.parent_key)) out.set(c.parent_key, c.parent_title || '')
  if (selected && selected !== 'none' && !out.has(selected)) out.set(selected, '')
  return [...out].map(([key, title]) => ({ key, title }))
}

// hasSession: o card já teve (ou tem) sessão.
export const hasSession = (card: Pick<CardSummary, 'session_count' | 'live'>) => card.session_count > 0 || !!card.live

// isBareBacklog: backlog sem sessão (e não-épico) mostra só chave, tipo e título,
// além do que você mesmo definiu (épico, pontos, prioridade, labels).
export function isBareBacklog(card: Pick<CardSummary, 'column' | 'type' | 'session_count' | 'live'>): boolean {
  return card.column === 'backlog' && card.type !== 'epico' && !hasSession(card)
}

type HandoffCard = Pick<CardSummary, 'column' | 'type' | 'session_count' | 'live' | 'signal' | 'readiness'>

// readyToResume: pausado com o handoff completo e sem bloqueio. É a mesma regra do
// selo "pronto para retomar" e da contagem da faixa do topo.
export function readyToResume(card: HandoffCard): boolean {
  return card.column === 'pausado' && card.type !== 'epico' && card.readiness.total > 0 && card.readiness.ready && card.signal !== 'bloqueado'
}

// handoffHint: o indicador de handoff só aparece quando informa algo: "pronto para
// retomar" (pausado e completo) ou "contexto n/5" (incompleto, inclusive 0/5, em card
// com sessão e ainda não concluído). Fora disso, vazio.
export function handoffHint(card: HandoffCard): string {
  const { met, total, ready } = card.readiness
  if (card.type === 'epico' || total <= 0) return ''
  if (readyToResume(card)) return 'pronto para retomar'
  if (!ready && met < total && hasSession(card) && card.column !== 'concluido') return `contexto ${met}/${total}`
  return ''
}

// ageText: "há 36 h" abaixo de 48 h, "há 3 dias" a partir daí; vazio sem data válida.
export function ageText(iso: string | null, now: Date = new Date()): string {
  if (!iso) return ''
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return ''
  const hours = Math.max(0, Math.floor((now.getTime() - t) / 3600000))
  return hours < 48 ? `há ${hours} h` : `há ${Math.floor(hours / 24)} dias`
}

// boardStrip: o que espera por você. review = cards em Revisão; oldestReview = o
// signal_since mais antigo entre os "aguardando revisão" (o dado só existe após 24 h
// sem atividade; sem ele, null); resumable = pausados prontos; live = sessões vivas.
export interface BoardStrip { review: number; oldestReview: string | null; resumable: number; live: number }
export function boardStrip(cards: CardSummary[]): BoardStrip {
  const out: BoardStrip = { review: 0, oldestReview: null, resumable: 0, live: 0 }
  for (const c of cards) {
    if (c.column === 'revisao') {
      out.review++
      if (c.signal === 'aguardando_revisao' && c.signal_since && !Number.isNaN(Date.parse(c.signal_since))
        && (out.oldestReview === null || Date.parse(c.signal_since) < Date.parse(out.oldestReview))) out.oldestReview = c.signal_since
    }
    if (readyToResume(c)) out.resumable++
    if (c.live) out.live++
  }
  return out
}

// DONE_VISIBLE: quantos concluídos a coluna mostra antes de "Ver os N concluídos".
export const DONE_VISIBLE = 3

// recentDone: os concluídos do mais recente (updated_at, que muda ao concluir) para o
// mais antigo, cortados em `limit`. Sem data válida vai para o fim, na ordem de entrada.
export function recentDone(cards: CardSummary[], limit: number = DONE_VISIBLE): CardSummary[] {
  const at = (c: CardSummary) => { const t = Date.parse(c.updated_at ?? ''); return Number.isNaN(t) ? -Infinity : t }
  return cards.map((c, i) => ({ c, i, t: at(c) }))
    .sort((a, b) => (a.t === b.t ? a.i - b.i : b.t > a.t ? 1 : -1))
    .slice(0, limit).map((x) => x.c)
}
