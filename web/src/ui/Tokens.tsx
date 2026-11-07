const dec = new Intl.NumberFormat('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

export function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${dec.format(n / 1_000_000)} M`
  if (n >= 1000) return `${Math.round(n / 1000)} k`
  return String(n)
}

export function Tokens({ n }: { n: number }) {
  return <span>{formatTokens(n)}</span>
}
