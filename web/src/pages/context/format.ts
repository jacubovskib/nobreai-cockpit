const dec = new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 1 })

// formatTokensPt: tokens em pt-BR curto ("620 mil", "1 milhão", "1,2 milhão").
export function formatTokensPt(n: number): string {
  if (n < 1000) return String(Math.round(n))
  const mil = n < 10_000 ? Math.round(n / 100) / 10 : Math.round(n / 1000)
  if (mil < 1000) return `${dec.format(mil)} mil`
  const m = Math.round(n / 100_000) / 10
  return `${dec.format(m)} ${m >= 2 ? 'milhões' : 'milhão'}`
}

export const pctText = (pct: number) => `${Math.round(pct)}%`
// A barra nunca passa de 100%.
export const barPct = (pct: number) => Math.max(0, Math.min(100, pct))
