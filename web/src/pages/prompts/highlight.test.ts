import { splitVars } from './highlight'

describe('splitVars', () => {
  it('separa texto e variável', () => {
    expect(splitVars('a {{card.id}} b')).toEqual([
      { text: 'a ', isVar: false },
      { text: '{{card.id}}', isVar: true },
      { text: ' b', isVar: false },
    ])
  })
  it('texto vazio e sem variáveis', () => {
    expect(splitVars('')).toEqual([])
    expect(splitVars('oi')).toEqual([{ text: 'oi', isVar: false }])
  })
  it('variáveis coladas', () => {
    expect(splitVars('{{a}}{{b}}').map((p) => p.isVar)).toEqual([true, true])
  })
})
