// Orçamento de terminais ligados na grade: decide quais tiles têm xterm
// montado e WebSocket aberto, para o navegador nunca renderizar mais que MAX_LIVE.

export const MAX_LIVE = 9
export const HIDDEN_PAUSE_MS = 30_000

export interface BudgetInput {
  ids: string[] // tiles da aba atual, na ordem da grade
  onScreen: Set<string> // IntersectionObserver
  ended: Set<string> // encerrados
  woken: Set<string> // encerrados que o usuário clicou para ligar
  focused: string | null
  lastUsed: Record<string, number> // ms da última interação; ausente = 0
  pageHidden: boolean // escondida há mais de HIDDEN_PAUSE_MS
}

export function liveSet(i: BudgetInput, max: number = MAX_LIVE): Set<string> {
  // Página escondida desliga tudo, inclusive o focado.
  if (i.pageHidden || max <= 0) return new Set()

  const live = new Set<string>()
  // O focado entra sempre (se for um tile da aba) e conta no orçamento.
  if (i.focused !== null && i.ids.includes(i.focused)) live.add(i.focused)

  const candidatos = i.ids
    .map((id, ordem) => ({ id, ordem }))
    .filter(
      ({ id }) =>
        id !== i.focused && i.onScreen.has(id) && (!i.ended.has(id) || i.woken.has(id)),
    )
    // Mais recente primeiro; empate fica na ordem da grade.
    .sort((a, b) => (i.lastUsed[b.id] ?? 0) - (i.lastUsed[a.id] ?? 0) || a.ordem - b.ordem)

  for (const { id } of candidatos) {
    if (live.size >= max) break
    live.add(id)
  }
  return live
}
