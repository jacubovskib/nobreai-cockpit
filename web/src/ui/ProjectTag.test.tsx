import { render, screen } from '@testing-library/react'
import { ProjectTag } from './ProjectTag'
import type { Project } from '../api/types'

const proj = (id: number, name: string, parent_id: number | null = null): Project =>
  ({ id, name, path: `/w/${name}`, capture: true, base_branch: '', parent_id, kind: 'repo' }) as Project

describe('ProjectTag', () => {
  it('mostra sempre o nome do projeto (a cor sozinha não basta)', () => {
    render(<ProjectTag project={proj(1, 'api')} projects={[proj(1, 'api')]} />)
    expect(screen.getByText('api')).toBeInTheDocument()
  })
  it('um filho aparece como "pai › app"', () => {
    const list = [proj(1, 'loja'), proj(2, 'web', 1)]
    render(<ProjectTag project={list[1]} projects={list} />)
    expect(screen.getByText('loja › web')).toBeInTheDocument()
  })
  it('sem projeto mostra "Sem projeto"', () => {
    render(<ProjectTag project={null} projects={[]} />)
    expect(screen.getByText('Sem projeto')).toBeInTheDocument()
  })
})
