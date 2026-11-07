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
}

export function invalidateForKind(qc: QueryClient, kind: string) {
  const keys = KIND_KEYS[kind]
  if (!keys) {
    qc.invalidateQueries()
    return
  }
  for (const k of keys) qc.invalidateQueries({ queryKey: [k] })
}

// useStream abre o EventSource uma vez e invalida o cache por kind. O
// EventSource reconecta sozinho se o daemon cair.
export function useStream() {
  const qc = useQueryClient()
  useEffect(() => {
    if (typeof EventSource === 'undefined') return
    const es = new EventSource('/api/stream')
    es.addEventListener('changed', (e) => {
      try {
        const { kind } = JSON.parse((e as MessageEvent).data) as { kind: string }
        invalidateForKind(qc, kind)
      } catch {
        qc.invalidateQueries()
      }
    })
    // Ao reconectar, o que mudou durante a queda não chegou: revalida tudo.
    es.addEventListener('open', () => qc.invalidateQueries())
    return () => es.close()
  }, [qc])
}
