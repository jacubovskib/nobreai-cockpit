import { useEffect, useRef, useState, type RefObject } from 'react'

const sameSet = (a: Set<string>, b: Set<string>) => a.size === b.size && [...a].every((x) => b.has(x))

// useOnScreen: ids dos tiles ([data-tile]) dentro de `root` que estão na tela
// ou a até 200 px dela. Sem IntersectionObserver no ambiente, todos contam.
// Os tiles são procurados de novo depois de cada render de quem chama, que é
// quem desenha a grade; o Set só muda quando o conteúdo muda.
export function useOnScreen(root: RefObject<HTMLElement>): Set<string> {
  const [ids, setIds] = useState<Set<string>>(() => new Set())
  const io = useRef<IntersectionObserver | null>(null)
  const seen = useRef(new Map<Element, string>())
  const visible = useRef(new Set<string>())

  const publish = (next: Set<string>) => setIds((prev) => (sameSet(prev, next) ? prev : next))

  useEffect(() => {
    if (typeof IntersectionObserver === 'undefined') return
    const obs = new IntersectionObserver((entries) => {
      for (const e of entries) {
        const id = seen.current.get(e.target)
        if (id === undefined) continue
        if (e.isIntersecting) visible.current.add(id)
        else visible.current.delete(id)
      }
      publish(new Set(visible.current))
    }, { rootMargin: '200px' })
    io.current = obs
    return () => {
      obs.disconnect()
      io.current = null
      seen.current.clear()
      visible.current.clear()
    }
  }, [])

  // Sem dependências de propósito: confere os tiles a cada render.
  useEffect(() => {
    const els = Array.from(root.current?.querySelectorAll('[data-tile]') ?? [])
    if (typeof IntersectionObserver === 'undefined') {
      publish(new Set(els.map((el) => el.getAttribute('data-tile') ?? '')))
      return
    }
    const obs = io.current
    if (!obs) return
    const now = new Set(els)
    let dropped = false
    for (const [el, id] of seen.current) {
      if (now.has(el)) continue
      obs.unobserve(el)
      seen.current.delete(el)
      dropped = visible.current.delete(id) || dropped
    }
    for (const el of els) {
      if (seen.current.has(el)) continue
      seen.current.set(el, el.getAttribute('data-tile') ?? '')
      obs.observe(el)
    }
    if (dropped) publish(new Set(visible.current))
  })

  return ids
}
