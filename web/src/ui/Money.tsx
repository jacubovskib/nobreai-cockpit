const fmt = new Intl.NumberFormat('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

// Money mostra custo em USD ("US$ 4,82"); com currency "BRL" e cotação > 0
// converte para "R$ x,xx". unpriced nunca vira zero: mostra "sem preço".
export function Money({
  usd,
  unpriced,
  currency,
  rate,
}: {
  usd: number
  unpriced?: boolean
  currency?: string
  rate?: number
}) {
  if (unpriced) return <span>sem preço</span>
  if (currency === 'BRL' && rate && rate > 0) return <span>{`R$ ${fmt.format(usd * rate)}`}</span>
  return <span>{`US$ ${fmt.format(usd)}`}</span>
}
