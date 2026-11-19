import { jaccard, similarTo } from './text'

describe('similaridade', () => {
  it('ignora palavras vazias como o servidor', () => {
    expect(jaccard('rode go test antes do commit', 'rode go test antes de commitar')).toBeGreaterThanOrEqual(0.6)
    expect(similarTo({ id: 2, rule: 'rode go test antes do commit' }, [{ id: 1, rule: 'rode go test antes de commitar' }])?.kind).toBe('parecida')
  })
  it('texto só de palavras vazias usa todas as palavras', () => {
    expect(jaccard('o que', 'o que')).toBe(1)
    expect(jaccard('o que', 'de para')).toBe(0)
  })
  it('vazio com vazio é 1', () => {
    expect(jaccard('', '!!')).toBe(1)
  })
})
