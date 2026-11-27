import { ApiError, post } from '../../api/client'
import type { Card, Memory, Project, Prompt } from '../../api/types'

// Tamanho máximo de cada grupo da paleta (Ctrl+K).
export const MAX_GROUP = 8

// Sem diferenciar maiúsculas nem acentos.
const norm = (s: string) => s.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase()
const matches = (q: string, ...fields: string[]) => {
  const n = norm(q)
  return fields.some((f) => norm(f).includes(n))
}

// Prompts da lista do #: do agente, globais ou do projeto (e do pai dele).
export function promptsFor(prompts: Prompt[], agent: string, project: Project | null, query: string): Prompt[] {
  return prompts
    .filter((p) => p.agents.includes(agent))
    .filter((p) => p.project_id === null || (project !== null && (p.project_id === project.id || p.project_id === project.parent_id)))
    .filter((p) => matches(query, p.name, p.slug, p.tag))
    .sort((a, b) => b.uses - a.uses || a.name.localeCompare(b.name))
}

export function insertAt(text: string, start: number, end: number, piece: string): { text: string; caret: number } {
  return { text: text.slice(0, start) + piece + text.slice(end), caret: start + piece.length }
}

export function cardPiece(c: Card): string {
  const head = `${c.key} · ${c.title}`
  return c.where_stopped ? `${head}\nOnde parei: ${c.where_stopped}` : head
}

export function memoryPiece(m: Memory): string {
  return m.do ? `${m.rule} — ${m.do}` : m.rule
}

export function paletteCards(cards: Card[], query: string): Card[] {
  return cards
    .filter((c) => c.column !== 'concluido')
    .filter((c) => matches(query, c.key, c.title))
    .sort((a, b) => (a.updated_at < b.updated_at ? 1 : a.updated_at > b.updated_at ? -1 : 0))
    .slice(0, MAX_GROUP)
}

// A lista por projeto já vem do servidor com as herdadas.
export function paletteMemories(mems: Memory[], project: Project | null, query: string): Memory[] {
  return mems
    .filter((m) => m.status === 'ativa')
    .filter((m) => project !== null || m.project_id === null)
    .filter((m) => matches(query, m.rule, m.do))
    .slice(0, MAX_GROUP)
}

// A edição trocou o texto inteiro (selecionar tudo e digitar, ou apagar tudo):
// o novo não tem nem começo nem fim em comum com o anterior.
export function replacedAll(prev: string, next: string): boolean {
  if (prev === next) return false
  if (prev === '' || next === '') return true
  return prev[0] !== next[0] && prev[prev.length - 1] !== next[next.length - 1]
}

// Texto que o agente leria como comando (#, / ou !) em vez de conversa.
export function commandWarning(text: string): boolean {
  return text !== '' && '#/!'.includes(text[0])
}

export async function sendInput(terminalId: string, text: string, prompt?: string): Promise<void> {
  await post('/api/terminals/' + terminalId + '/input', { text, prompt: prompt ?? '' })
}

// O 404 de terminal inexistente e o de rota ausente são indistinguíveis: a
// mensagem do daemon antigo vale para os dois.
export function inputError(e: unknown): string {
  const status = e instanceof ApiError ? e.status : 0
  if (status === 404 || status === 405) return 'Este daemon não aceita entrada pela barra. Atualize o Cockpit.'
  if (status === 409) return 'Terminal encerrado.'
  if (status === 413) return 'Texto grande demais para enviar.'
  if (status === 504) return 'O agente não leu a entrada. Tente de novo.'
  return 'Não foi possível enviar.'
}
