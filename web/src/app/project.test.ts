import { displayName, effectiveIds, familyIds, projectColor, projectForPath, withProject } from './project'
import type { Project } from '../api/types'

const P = (id: number, path = '/p' + id): Project => ({ id, name: 'p' + id, path, capture: true, base_branch: '', parent_id: null, kind: 'repo' })

describe('withProject', () => {
  it('acrescenta o parâmetro respeitando ? existente', () => {
    expect(withProject('/board', 3)).toBe('/board?projeto=3')
    expect(withProject('/worktrees/detalhe?path=x', 3)).toBe('/worktrees/detalhe?path=x&projeto=3')
    expect(withProject('/board', null)).toBe('/board')
  })
  it('carrega o app quando houver', () => {
    expect(withProject('/board', 3, 7)).toBe('/board?projeto=3&app=7')
    expect(withProject('/board', 3, 'geral')).toBe('/board?projeto=3&app=geral')
    expect(withProject('/board?x=1', 3, 'geral')).toBe('/board?x=1&projeto=3&app=geral')
    expect(withProject('/board', null, 7)).toBe('/board')
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

describe('família', () => {
  const loja: Project = { ...P(1, '/loja'), name: 'loja' }
  const web: Project = { ...P(2, '/loja/web'), name: 'web', parent_id: 1 }
  const api: Project = { ...P(3, '/loja/api'), name: 'api', parent_id: 1 }
  const outro = P(4)
  const all = [loja, web, api, outro]
  it('familyIds of parent includes children', () => {
    expect(familyIds(all, 1)).toEqual([1, 2, 3])
  })
  it('familyIds of child is only itself', () => {
    expect(familyIds(all, 2)).toEqual([2])
  })
  it('displayName of child is "loja › web"', () => {
    expect(displayName(web, all)).toBe('loja › web')
    expect(displayName(loja, all)).toBe('loja')
  })
})

describe('effectiveIds', () => {
  const loja: Project = { ...P(1, '/loja'), name: 'loja' }
  const web: Project = { ...P(2, '/loja/web'), name: 'web', parent_id: 1 }
  const api: Project = { ...P(3, '/loja/api'), name: 'api', parent_id: 1 }
  const solo = P(4)
  const all = [loja, web, api, solo]
  it('Todos é a família inteira', () => {
    expect(effectiveIds(all, loja, null)).toEqual([1, 2, 3])
  })
  it('geral é só o principal', () => {
    expect(effectiveIds(all, loja, 'geral')).toEqual([1])
  })
  it('um app é só ele', () => {
    expect(effectiveIds(all, loja, 3)).toEqual([3])
  })
  it('projeto sem filhos é só ele', () => {
    expect(effectiveIds(all, solo, null)).toEqual([4])
  })
})
