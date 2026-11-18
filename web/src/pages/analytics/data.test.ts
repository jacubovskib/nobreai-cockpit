import { contextLabel } from './data'

describe('contextLabel', () => {
  it('Bash vira o comando com prompt', () => expect(contextLabel('Bash', 'git status')).toBe('$ git status'))
  it('hook ganha o prefixo', () => expect(contextLabel('hook', 'SessionStart · cockpit')).toBe('hook SessionStart · cockpit'))
  it('demais ferramentas usam a chave como está', () => {
    expect(contextLabel('Read', 'Read .png')).toBe('Read .png')
    expect(contextLabel('Agent', 'Agent Explore')).toBe('Agent Explore')
  })
  it('chave vazia cai no nome da ferramenta', () => expect(contextLabel('Write', '')).toBe('Write'))
})
