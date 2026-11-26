// Carga sintética e contas da bancada de performance dos terminais.
// Tudo aqui é puro (sem DOM), para rodar no worker e nos testes.

export type Profile = 'agente' | 'enxurrada'

const enc = new TextEncoder()

// Metas da bancada (ms): tarefa longa máxima, atraso p95 e atraso máximo da
// thread principal.
export const META = { longMax: 50, lagP95: 50, lagMax: 100 }

// intervalMs é o período entre dois ticks do perfil.
export function intervalMs(p: Profile): number {
  return p === 'agente' ? 80 : 10
}

const ESC = '\x1b'

// tick é o que um terminal recebe no k-ésimo período do perfil.
// agente: redesenha 8 linhas (sobe 8, reescreve) e, a cada 25 ticks, solta 30
// linhas novas, como a barra de status de um agente. enxurrada: 20 linhas de
// 120 colunas, como um `cat` de arquivo grande.
export function tick(p: Profile, k: number): Uint8Array {
  let s = ''
  if (p === 'agente') {
    s += `${ESC}[8A`
    for (let i = 0; i < 8; i++) s += `${ESC}[2K│ passo ${k} linha ${i} ${'.'.repeat((k + i) % 40)}\r\n`
    if (k % 25 === 0) {
      for (let i = 0; i < 30; i++) s += `saída ${k}.${i}: ${'lorem ipsum dolor sit amet '.repeat(2)}\r\n`
    }
  } else {
    for (let i = 0; i < 20; i++) {
      const head = `${k}:${i} `
      s += head + 'abcdefghij'.repeat(12).slice(0, 120 - head.length) + '\r\n'
    }
  }
  return enc.encode(s)
}

// percentile é o percentil p (0-100) pelo método do posto mais próximo.
export function percentile(xs: number[], p: number): number {
  if (xs.length === 0) return 0
  const s = [...xs].sort((a, b) => a - b)
  return s[Math.max(0, Math.ceil((p / 100) * s.length) - 1)]
}

export function verdict(m: { longMax: number; lagP95: number; lagMax: number }): 'aprovado' | 'reprovado' {
  return m.longMax < META.longMax && m.lagP95 < META.lagP95 && m.lagMax < META.lagMax ? 'aprovado' : 'reprovado'
}
