import { useEffect, useState } from 'react'
import { HIDDEN_PAUSE_MS } from './liveBudget'

// usePageHidden: true só depois de `ms` com a página escondida, para uma troca
// rápida de aba não desligar a grade; volta a false na hora em que ela aparece.
export function usePageHidden(ms: number = HIDDEN_PAUSE_MS): boolean {
  const [hidden, setHidden] = useState(false)
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined
    const sync = () => {
      clearTimeout(timer)
      timer = undefined
      if (document.hidden) timer = setTimeout(() => setHidden(true), ms)
      else setHidden(false)
    }
    sync()
    document.addEventListener('visibilitychange', sync)
    return () => {
      clearTimeout(timer)
      document.removeEventListener('visibilitychange', sync)
    }
  }, [ms])
  return hidden
}
