import { draftFor, dropDraft, resetDrafts, saveFreed, saveText } from './inputDrafts'

beforeEach(() => resetDrafts())

describe('inputDrafts', () => {
  it('terminal sem rascunho: texto vazio e entrada não liberada', () => {
    expect(draftFor('t1')).toEqual({ text: '', freed: false })
  })

  it('guarda texto e Liberar entrada por terminal, sem misturar', () => {
    saveText('t1', 'oi')
    saveFreed('t1')
    saveText('t2', 'outro')
    expect(draftFor('t1')).toEqual({ text: 'oi', freed: true })
    expect(draftFor('t2')).toEqual({ text: 'outro', freed: false })
  })

  it('salvar o texto mantém o Liberar entrada e vice-versa', () => {
    saveFreed('t1')
    saveText('t1', 'a')
    saveText('t1', 'ab')
    expect(draftFor('t1')).toEqual({ text: 'ab', freed: true })
  })

  it('dropDraft esquece o terminal', () => {
    saveText('t1', 'oi')
    saveFreed('t1')
    dropDraft('t1')
    expect(draftFor('t1')).toEqual({ text: '', freed: false })
  })

  it('o vazio devolvido não é compartilhado por escrita', () => {
    const d = draftFor('t1')
    saveText('t2', 'x')
    expect(d).toEqual({ text: '', freed: false })
  })
})
