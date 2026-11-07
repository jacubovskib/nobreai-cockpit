import { render, screen, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { WorktreeDetailPage } from './WorktreeDetail'
import type { WorktreeDetail } from '../api/types'
import { ago, res, session, wt } from './worktreeFixtures'

const total = { key: '', agent: '', day: '', sessions: 1, input: 1200, output: 300, cache_write: 0, cache_read: 0, cost_usd: 0.42, unpriced: false }
const detailURL = '/api/worktrees/detail?project=1&path=%2Fw%2Fapi-oauth'

function mockApi(routes: Record<string, unknown>) {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const r = routes[url]
    if (r === undefined) return new Response(JSON.stringify({ error: 'worktree não encontrado' }), { status: 404 })
    return new Response(JSON.stringify(r), { status: 200 })
  }))
}
function wrap(path = '/worktrees/detalhe?project=1&path=%2Fw%2Fapi-oauth') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes><Route path="/worktrees/detalhe" element={<WorktreeDetailPage />} /></Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}
afterEach(() => vi.unstubAllGlobals())

const detail: WorktreeDetail = {
  project: { id: 1, name: 'api', path: '/w/api', capture: true },
  worktree: {
    ...wt, path: '/w/api-oauth', branch: 'feat/oauth', main: false, locked: true, lock_reason: 'disco externo', cards: [], sessions: [],
    resources: res({ cpu_pct: 180, rss_bytes: 1288490188, procs: 3, disk: { bytes: 3650722201, measured_at: ago(4), pending: false } }),
  },
  processes: [
    { pid: 20, comm: 'node', cpu_pct: 120, rss_bytes: 943718400, cwd: '/w/api-oauth/web' },
    { pid: 21, comm: 'esbuild', cpu_pct: 60, rss_bytes: 104857600, cwd: '/tmp' },
    { pid: 22, comm: 'zsh', cpu_pct: 0, rss_bytes: 5242880, cwd: '/w/api-oauth' },
  ],
  git: {
    staged: 1, modified: 2, untracked: 1, conflicts: 0, total_files: 4, base: 'main', compared: true, ahead: 3, behind: 1, error: '',
    files: [{ path: 'auth/oauth.go', code: ' M' }, { path: 'auth/token.go', code: 'M ' }, { path: 'notas.md', code: '??' }, { path: 'web/app.ts', code: ' M' }],
    commits: [{ hash: 'f00dbabe00000000000000000000000000000000', subject: 'callback do OAuth', author: 'Breno', at: ago(30) }],
  },
  sessions: [
    { ...session({ id: 'claude:oauth', worktree: '/w/api-oauth', worktree_linked: true, branch: 'feat/oauth', last_activity: 'Edit auth/oauth.go',
      card: { key: 'NAC-7', title: 'Login com OAuth', column: 'em_andamento' } }), usage: total, unpriced: false },
    { ...session({ id: 'claude:antiga', live: false, ended_at: ago(4320), started_at: ago(4400) }), usage: { ...total, cost_usd: 1.5 }, unpriced: false },
  ],
  cards: [{ key: 'NAC-7', title: 'Login com OAuth', column: 'em_andamento' }, { key: 'NAC-2', title: 'Base do OAuth', column: 'concluido' }],
  events: [
    { id: 2, session_id: 'claude:oauth', agent: 'claude', kind: 'file_edit', text: 'Edit auth/oauth.go', at: ago(1) },
    { id: 1, session_id: 'claude:oauth', agent: 'claude', kind: 'command', text: 'Bash go test ./auth/...', at: ago(3) },
  ],
}

describe('WorktreeDetailPage', () => {
  it('mostra identidade, recursos, processos, git, sessões, cards e atividade', async () => {
    mockApi({ [detailURL]: detail, '/api/settings': {} })
    wrap()
    expect(await screen.findByRole('heading', { level: 1, name: 'api-oauth' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Worktrees' })).toHaveAttribute('href', '/worktrees')
    expect(screen.getByText('travado: disco externo')).toBeInTheDocument()
    expect(screen.getByText('a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0')).toBeInTheDocument()

    const recursos = screen.getByRole('region', { name: 'Recursos' })
    expect(within(recursos).getByText('15%')).toBeInTheDocument()
    expect(within(recursos).getByText('3,4 GB')).toBeInTheDocument()
    const procs = within(recursos).getByRole('table', { name: 'Processos' })
    expect(within(procs).getAllByRole('row')).toHaveLength(4) // cabeçalho + 3
    expect(within(procs).getByText('/tmp')).toBeInTheDocument()

    const git = screen.getByRole('region', { name: 'Git' })
    expect(within(git).getByText(/3 à frente e 1 atrás de/)).toBeInTheDocument()
    expect(within(git).getByText('main')).toBeInTheDocument()
    expect(within(git).getByText(/1 preparado · 2 modificados · 1 novo/)).toBeInTheDocument()
    expect(within(git).getByText('auth/oauth.go')).toBeInTheDocument()
    expect(within(git).getByText('callback do OAuth')).toBeInTheDocument()

    const sess = screen.getByRole('region', { name: 'Sessões' })
    expect(within(sess).getAllByRole('row')).toHaveLength(3)
    expect(within(sess).getByRole('link', { name: /NAC-7/ })).toHaveAttribute('href', '/cards/NAC-7')
    expect(within(sess).getByText(/encerrada há 3 d/)).toBeInTheDocument()

    const cards = screen.getByRole('region', { name: 'Cards do branch' })
    expect(within(cards).getAllByRole('link')).toHaveLength(2)

    const atividade = screen.getByRole('region', { name: 'Atividade recente' })
    expect(within(atividade).getByText('Bash go test ./auth/...')).toBeInTheDocument()
  })

  it('worktree inexistente mostra aviso e volta para a lista', async () => {
    mockApi({ '/api/settings': {} })
    wrap('/worktrees/detalhe?project=1&path=%2Fnao%2Fexiste')
    expect(await screen.findByRole('heading', { name: 'Worktree não encontrado' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Voltar aos worktrees/ })).toHaveAttribute('href', '/worktrees')
  })

  it('git com erro e sem recursos não quebra a página', async () => {
    const bare = { ...detail, worktree: { ...detail.worktree, resources: null }, processes: [], git: { ...detail.git!, error: 'o diretório do worktree não existe mais', files: [], commits: [], compared: false } }
    mockApi({ [detailURL]: bare, '/api/settings': {} })
    wrap()
    expect(await screen.findByText('o diretório do worktree não existe mais')).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Recursos' })).not.toBeInTheDocument()
  })
})
