import { act, renderHook } from '@testing-library/react'
import { usePageHidden } from './usePageHidden'

let hidden = false
beforeEach(() => {
  vi.useFakeTimers()
  hidden = false
  vi.spyOn(document, 'hidden', 'get').mockImplementation(() => hidden)
})
afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

const setHidden = (v: boolean) => act(() => {
  hidden = v
  document.dispatchEvent(new Event('visibilitychange'))
})

describe('usePageHidden', () => {
  it('só vira true depois de 30 s escondida e volta a false na hora em que a página aparece', () => {
    const { result } = renderHook(() => usePageHidden())
    expect(result.current).toBe(false)
    setHidden(true)
    act(() => { vi.advanceTimersByTime(29_999) })
    expect(result.current).toBe(false)
    act(() => { vi.advanceTimersByTime(1) })
    expect(result.current).toBe(true)
    setHidden(false)
    expect(result.current).toBe(false)
  })

  it('uma troca rápida de aba não conta: o prazo recomeça a cada vez que esconde', () => {
    const { result } = renderHook(() => usePageHidden(1000))
    setHidden(true)
    act(() => { vi.advanceTimersByTime(900) })
    setHidden(false)
    setHidden(true)
    act(() => { vi.advanceTimersByTime(900) })
    expect(result.current).toBe(false)
    act(() => { vi.advanceTimersByTime(100) })
    expect(result.current).toBe(true)
  })

  it('aberta já escondida também conta o prazo', () => {
    hidden = true
    const { result } = renderHook(() => usePageHidden(1000))
    expect(result.current).toBe(false)
    act(() => { vi.advanceTimersByTime(1000) })
    expect(result.current).toBe(true)
  })
})
