import { QueryClient } from '@tanstack/react-query'
import { createInvalidator } from './useStream'

describe('createInvalidator', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  function setup() {
    const qc = new QueryClient()
    const spy = vi.spyOn(qc, 'invalidateQueries').mockResolvedValue()
    return { qc, spy, inv: createInvalidator(qc, 250) }
  }
  const keysOf = (spy: ReturnType<typeof setup>['spy']) =>
    spy.mock.calls.map((c) => (c[0] as { queryKey: string[] }).queryKey[0])

  it('junta eventos do mesmo kind numa invalidação depois de 250 ms', () => {
    const { spy, inv } = setup()
    inv.push('prompts')
    inv.push('prompts')
    vi.advanceTimersByTime(200)
    inv.push('prompts')
    expect(spy).not.toHaveBeenCalled()
    vi.advanceTimersByTime(50)
    expect(keysOf(spy)).toEqual(['prompts'])
    vi.advanceTimersByTime(1000)
    expect(spy).toHaveBeenCalledTimes(1)
  })

  it('kinds diferentes não se juntam', () => {
    const { spy, inv } = setup()
    inv.push('prompts')
    inv.push('cards')
    vi.advanceTimersByTime(250)
    expect(keysOf(spy)).toEqual(['prompts', 'cards', 'card', 'overview', 'events', 'worktrees'])
  })

  it('cancel descarta o que estava pendente', () => {
    const { spy, inv } = setup()
    inv.push('cards')
    inv.cancel()
    vi.advanceTimersByTime(1000)
    expect(spy).not.toHaveBeenCalled()
  })
})
