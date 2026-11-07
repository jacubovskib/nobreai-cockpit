export interface Part { text: string; isVar: boolean }

// splitVars separa o texto em pedaços de texto puro e variáveis {{nome}}.
export function splitVars(body: string): Part[] {
  const parts: Part[] = []
  for (const s of body.split(/(\{\{[^{}]+\}\})/)) {
    if (s === '') continue
    parts.push({ text: s, isVar: /^\{\{[^{}]+\}\}$/.test(s) })
  }
  return parts
}

// Variáveis que o daemon preenche a partir do card (internal/resume.Vars).
export const KNOWN_VARS = [
  'card.id', 'card.titulo', 'card.onde_parei', 'card.pacote',
  'card.arquivos', 'card.como_testar', 'git.branch', 'handoff.faltando',
] as const

// varName tira as chaves de "{{ nome }}".
export const varName = (v: string) => v.replace(/^\{\{\s*|\s*\}\}$/g, '')
