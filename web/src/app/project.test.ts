import { projectColor, projectForPath, withProject } from './project'
import type { Project } from '../api/types'

const P = (id: number, path = '/p' + id): Project => ({ id, name: 'p' + id, path, capture: true, base_branch: '' })

describe('withProject', () => {
  it('acrescenta o parâmetro respeitando ? existente', () => {
    expect(withProject('/board', 3)).toBe('/board?projeto=3')
    expect(withProject('/worktrees/detalhe?path=x', 3)).toBe('/worktrees/detalhe?path=x&projeto=3')
    expect(withProject('/board', null)).toBe('/board')
  })
})

describe('projectColor', () => {
  it('usa a posição por id, em ciclo', () => {
    const ps = [P(5), P(2), P(9)]
    expect(projectColor(2, ps)).toBe('#B07CE8')
    expect(projectColor(5, ps)).toBe('#C9A227')
    expect(projectColor(9, ps)).toBe('#E26D9C')
    const sete = [1, 2, 3, 4, 5, 6, 7].map((i) => P(i))
    expect(projectColor(7, sete)).toBe('#B07CE8')
  })
})

describe('projectForPath', () => {
  const ps = [P(1, '/a'), P(2, '/a/b')]
  it('prefixo mais longo respeitando o limite de diretório', () => {
    expect(projectForPath(ps, '/a/b/c')?.path).toBe('/a/b')
    expect(projectForPath(ps, '/ab')).toBeNull()
    expect(projectForPath(ps, '/a')?.path).toBe('/a')
  })
})
