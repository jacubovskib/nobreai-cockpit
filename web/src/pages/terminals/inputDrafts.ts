// Rascunho da barra de entrada de cada terminal, fora da árvore do tile: em
// pausa o tile desmonta a barra, e o texto e o "Liberar entrada" voltam quando
// ele liga de novo. Vive até recarregar a página. Sem assinantes: só a barra
// lê (ao montar) e escreve (a cada edição).
export interface Draft {
  text: string
  freed: boolean // "Liberar entrada" num terminal de `nobre run`
}

const EMPTY: Draft = { text: '', freed: false }
const drafts = new Map<string, Draft>()

export function draftFor(terminalId: string): Draft {
  return drafts.get(terminalId) ?? EMPTY
}

export function saveText(terminalId: string, text: string): void {
  drafts.set(terminalId, { ...draftFor(terminalId), text })
}

export function saveFreed(terminalId: string): void {
  drafts.set(terminalId, { ...draftFor(terminalId), freed: true })
}

// Terminal encerrado: o rascunho não tem mais para onde ir.
export function dropDraft(terminalId: string): void {
  drafts.delete(terminalId)
}

// Só para os testes.
export function resetDrafts(): void {
  drafts.clear()
}
