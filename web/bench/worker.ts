// Gera a saída sintética fora da thread principal. Faz o papel do daemon com
// batch=1: junta o que cada terminal produziu em janelas de 100 ms e manda um
// bloco por terminal por janela.
import { intervalMs, tick, type Profile } from './load'

export type ToWorker = { cmd: 'start'; profile: Profile; tiles: number } | { cmd: 'stop' }
export type FromWorker = { chunks: (ArrayBuffer | null)[] }

const BATCH_MS = 100

let timers: ReturnType<typeof setInterval>[] = []

function stop() {
  for (const t of timers) clearInterval(t)
  timers = []
}

self.onmessage = (e: MessageEvent<ToWorker>) => {
  stop()
  if (e.data.cmd !== 'start') return
  const { profile, tiles } = e.data
  const pending: Uint8Array[][] = Array.from({ length: tiles }, () => [])
  // k parte de um valor diferente por tile: os 9 fluxos não soltam rajadas juntos
  let k = 0
  timers.push(setInterval(() => {
    k++
    for (let i = 0; i < tiles; i++) pending[i].push(tick(profile, k + i * 3))
  }, intervalMs(profile)))
  timers.push(setInterval(() => {
    const chunks = pending.map((list) => {
      if (list.length === 0) return null
      const all = new Uint8Array(list.reduce((n, c) => n + c.byteLength, 0))
      let off = 0
      for (const c of list) { all.set(c, off); off += c.byteLength }
      list.length = 0
      return all.buffer as ArrayBuffer
    })
    const msg: FromWorker = { chunks }
    ;(self as unknown as Worker).postMessage(msg, chunks.filter((c): c is ArrayBuffer => c !== null))
  }, BATCH_MS))
}
