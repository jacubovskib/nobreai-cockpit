import { formatTokensPt, pctText } from './format'

describe('formatTokensPt', () => {
  it.each([
    [0, '0'], [850, '850'], [1500, '1,5 mil'], [62000, '62 mil'], [620000, '620 mil'], [999600, '1 milhão'],
    [1_000_000, '1 milhão'], [1_200_000, '1,2 milhão'], [2_000_000, '2 milhões'], [2_500_000, '2,5 milhões'],
  ])('%s -> %s', (n, txt) => expect(formatTokensPt(n)).toBe(txt))
})

describe('pctText', () => {
  it('arredonda e não passa de 100 na barra', () => {
    expect(pctText(62.4)).toBe('62%')
    expect(pctText(0)).toBe('0%')
  })
})
