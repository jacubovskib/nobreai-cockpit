const one = new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 1 })

// formatBytes: "1,2 GB", "850 MB", "12 KB" (base 1024, uma casa decimal).
export function formatBytes(n: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${i === 0 ? Math.round(v) : one.format(v)} ${units[i]}`
}

// formatPct: "12%", "0,4%" (uma casa abaixo de 10).
export function formatPct(n: number): string {
  return `${n < 10 ? one.format(n) : Math.round(n)}%`
}
