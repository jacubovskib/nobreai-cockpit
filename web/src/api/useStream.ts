import { useEffect } from 'react'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'

// Cada kind do stream invalida as queries cujo primeiro elemento da chave
// está na lista. Kinds desconhecidos invalidam tudo.
const KIND_KEYS: Record<string, string[]> = {
  cards: ['cards', 'card', 'overview', 'events'],
  sessions: ['sessions', 'card', 'cards', 'overview', 'usage', 'events'],
  config: ['settings', 'limits', 'prices', 'projects', 'overview'],
  usage: ['usage', 'overview', 'cards', 'card'],
  prompts: ['prompts'],
  terminals: ['terminals'],
}

export function invalidateForKind(qc: QueryClient, kind: string) {
  const keys = KIND_KEYS[kind]
  if (!keys) {
    qc.invalidateQueries()
    return
  }
  for (const k of keys) qc.invalidateQueries({ queryKey: [k] })
}

// DEBOUNCE_MS junta as rajadas do stream: uma sessão ativa publica vários
// eventos por segundo e cada um refaria as mesmas consultas.
export const DEBOUNCE_MS = 250

// createInvalidator agrupa as invalidações por kind: o primeiro evento de um
// kind agenda a invalidação para daqui a delay ms e os seguintes, até lá, se
// juntam a ela (sem reiniciar o prazo, para um fluxo contínuo não adiar a
// atualização para sempre).
export function createInvalidator(qc: QueryClient, delay = DEBOUNCE_MS) {
  const pending = new Map<string, ReturnType<typeof setTimeout>>()
  return {
    push(kind: string) {
      if (pending.has(kind)) return
      pending.set(
        kind,
        setTimeout(() => {
          pending.delete(kind)
          invalidateForKind(qc, kind)
        }, delay),
      )
    },
    cancel() {
      for (const t of pending.values()) clearTimeout(t)
      pending.clear()
    },
  }
}

// useStream abre o EventSource uma vez e invalida o cache por kind (com
// debounce). O EventSource reconecta sozinho se o daemon cair.
export function useStream() {
  const qc = useQueryClient()
  useEffect(() => {
    if (typeof EventSource === 'undefined') return
    const inv = createInvalidator(qc)
    const es = new EventSource('/api/stream')
    es.addEventListener('changed', (e) => {
      try {
        const { kind } = JSON.parse((e as MessageEvent).data) as { kind: string }
        inv.push(kind)
      } catch {
        inv.push('*')
      }
    })
    // Ao reconectar, o que mudou durante a queda não chegou: revalida tudo.
    es.addEventListener('open', () => qc.invalidateQueries())
    return () => {
      inv.cancel()
      es.close()
    }
  }, [qc])
}
