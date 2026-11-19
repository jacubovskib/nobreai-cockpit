export const RULE_MAX = 300
export const DO_MAX = 400

export const CATEGORIES: { id: string; label: string }[] = [
  { id: 'execucao', label: 'Execução e validação' },
  { id: 'shell', label: 'Shell e comandos' },
  { id: 'dominio', label: 'Regras do domínio' },
  { id: 'diretiva', label: 'Diretivas do usuário' },
]

export const len = (s: string) => [...s].length

// Mesma normalização do servidor: minúsculas, sem acento, sem pontuação, espaços simples.
export function normalize(s: string): string {
  return s
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, ' ')
    .trim()
}

// Palavras vazias do português, como em internal/memory/memory.go.
const STOPWORDS = new Set(['a', 'o', 'as', 'os', 'um', 'uma', 'de', 'do', 'da', 'dos', 'das', 'em', 'no', 'na', 'nos', 'nas', 'e', 'ou', 'para', 'por', 'com', 'que'])

function wordSet(s: string): Set<string> {
  const all = normalize(s).split(' ').filter(Boolean)
  const m = new Set(all.filter((w) => !STOPWORDS.has(w)))
  return m.size === 0 ? new Set(all) : m
}

export function jaccard(a: string, b: string): number {
  const x = wordSet(a)
  const y = wordSet(b)
  if (x.size === 0 && y.size === 0) return 1
  let inter = 0
  for (const w of x) if (y.has(w)) inter++
  const union = x.size + y.size - inter
  return union === 0 ? 0 : inter / union
}

export interface Similar { kind: 'duplicada' | 'parecida'; id: number }

// similarTo: só a regra, contra as de id menor (a mais antiga é a referência).
export function similarTo(item: { id: number; rule: string }, others: { id: number; rule: string }[]): Similar | null {
  const n = normalize(item.rule)
  let near: Similar | null = null
  for (const o of others) {
    if (o.id >= item.id) continue
    if (normalize(o.rule) === n) return { kind: 'duplicada', id: o.id }
    if (!near && jaccard(item.rule, o.rule) >= 0.6) near = { kind: 'parecida', id: o.id }
  }
  return near
}
