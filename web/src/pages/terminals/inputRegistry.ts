// Registro das barras de entrada dos tiles: a paleta (Ctrl+K) e a lista do #
// falam com a barra do terminal focado por aqui, sem passar por props.
export interface TileInputHandle {
  insert(piece: string): void // insere na posição do cursor e foca a barra
  pickPrompt(slug: string): void // mesmo efeito de escolher na lista do #
  focus(): void
}

const handles = new Map<string, TileInputHandle>()

// Devolve o cancelamento: só apaga se o registrado ainda for o mesmo handle.
export function registerInput(terminalId: string, h: TileInputHandle): () => void {
  handles.set(terminalId, h)
  return () => {
    if (handles.get(terminalId) === h) handles.delete(terminalId)
  }
}

export function inputFor(terminalId: string | null): TileInputHandle | null {
  return terminalId === null ? null : (handles.get(terminalId) ?? null)
}
