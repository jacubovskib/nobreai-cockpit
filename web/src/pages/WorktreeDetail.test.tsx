import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { WorktreeDetailPage } from './WorktreeDetail'
import { Worktrees } from './Worktrees'
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
  project: { id: 1, name: 'api', path: '/w/api', capture: true, base_branch: '' },
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
    // A lista rola dentro do painel em vez de esticar a coluna.
    const eventos = within(atividade).getByRole('list', { name: 'Eventos recentes' })
    expect(eventos).toHaveStyle({ overflowY: 'auto', maxHeight: '520px' })
    expect(eventos).toHaveAttribute('tabindex', '0')
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

  describe('excluir worktree', () => {
    const free: WorktreeDetail = {
      ...detail,
      worktree: { ...detail.worktree, locked: false, lock_reason: '' },
      sessions: detail.sessions.map((x) => ({ ...x, live: false })),
      git: { ...detail.git!, ahead: 0 },
    }
    type Call = { url: string; method: string; body: unknown; ct: string | null }
    function mockDelete(detailBody: WorktreeDetail, answers: Array<{ status: number; body: unknown }>, log: string[] = []) {
      const calls: Call[] = []
      vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
        const method = init?.method ?? 'GET'
        log.push(`${method} ${url}`)
        if (method === 'DELETE') {
          calls.push({ url, method, body: JSON.parse(String(init?.body)), ct: new Headers(init?.headers).get('Content-Type') })
          const a = answers.shift()!
          return new Response(JSON.stringify(a.body), { status: a.status })
        }
        if (url === detailURL) return new Response(JSON.stringify(detailBody), { status: 200 })
        if (url === '/api/worktrees' || url === '/api/projects') return new Response('[]', { status: 200 })
        return new Response('{}', { status: 200 })
      }))
      return calls
    }
    const question = (_: string, el: Element | null) => el?.tagName === 'SPAN' && el.textContent === 'Excluir a pasta /w/api-oauth?'
    const ok = { status: 200, body: { removed: true, branch_deleted: true, branch_kept_reason: '' } }

    it('não mostra Excluir no checkout principal', async () => {
      mockDelete({ ...free, worktree: { ...free.worktree, main: true } }, [])
      wrap()
      await screen.findByRole('heading', { level: 1, name: 'api-oauth' })
      expect(screen.queryByRole('button', { name: 'Excluir worktree' })).not.toBeInTheDocument()
    })

    it('desabilita Excluir com sessão viva e explica', async () => {
      mockDelete({ ...free, sessions: detail.sessions }, [])
      wrap()
      expect(await screen.findByRole('button', { name: 'Excluir worktree' })).toBeDisabled()
      expect(screen.getByText('Há uma sessão viva neste worktree.')).toBeInTheDocument()
    })

    it('desabilita Excluir com worktree travado e explica', async () => {
      mockDelete({ ...free, worktree: { ...free.worktree, locked: true } }, [])
      wrap()
      expect(await screen.findByRole('button', { name: 'Excluir worktree' })).toBeDisabled()
      expect(screen.getByText('Worktree travado.')).toBeInTheDocument()
    })

    it('confirma inline e envia DELETE com project, path e delete_branch', async () => {
      const calls = mockDelete(free, [ok])
      wrap()
      fireEvent.click(await screen.findByRole('button', { name: 'Excluir worktree' }))
      expect(screen.getByText(question)).toBeInTheDocument()
      expect(screen.getByText('Arquivos ignorados pelo git (como .env e node_modules) também serão apagados.')).toBeInTheDocument()
      fireEvent.click(screen.getByRole('checkbox', { name: 'Também apagar o branch feat/oauth' }))
      fireEvent.click(screen.getByRole('button', { name: 'Sim, excluir' }))
      await waitFor(() => expect(calls).toHaveLength(1))
      expect(calls[0]).toEqual({ url: '/api/worktrees', method: 'DELETE', ct: 'application/json', body: { project: 1, path: '/w/api-oauth', force: false, delete_branch: true } })
    })

    it('Cancelar fecha a confirmação sem enviar', async () => {
      const calls = mockDelete(free, [])
      wrap()
      fireEvent.click(await screen.findByRole('button', { name: 'Excluir worktree' }))
      fireEvent.click(screen.getByRole('button', { name: 'Cancelar' }))
      expect(screen.queryByText(question)).not.toBeInTheDocument()
      expect(calls).toHaveLength(0)
    })

    it('needs_confirm lista arquivos e reenvia com force', async () => {
      const files = Array.from({ length: 12 }, (_, i) => ({ path: `f${i}.go`, code: ' M' }))
      const calls = mockDelete(free, [
        { status: 409, body: { error: 'confirme', needs_confirm: true, files, total_files: 14, processes: [{ pid: 77, name: 'node' }] } },
        ok,
      ])
      wrap()
      fireEvent.click(await screen.findByRole('button', { name: 'Excluir worktree' }))
      fireEvent.click(screen.getByRole('button', { name: 'Sim, excluir' }))
      expect(await screen.findByText('O worktree tem alterações não commitadas ou processos rodando:')).toBeInTheDocument()
      expect(screen.getByText('M f0.go')).toBeInTheDocument()
      expect(screen.getByText('M f9.go')).toBeInTheDocument()
      expect(screen.queryByText('M f10.go')).not.toBeInTheDocument()
      expect(screen.getByText('+4')).toBeInTheDocument()
      expect(screen.getByText('node (pid 77)')).toBeInTheDocument()
      fireEvent.click(screen.getByRole('button', { name: 'Excluir mesmo assim' }))
      await waitFor(() => expect(calls).toHaveLength(2))
      expect(calls[1].body).toEqual({ project: 1, path: '/w/api-oauth', force: true, delete_branch: false })
    })

    it('erro do servidor aparece em alerta', async () => {
      mockDelete(free, [{ status: 409, body: { error: 'há outro worktree dentro deste (/w/api-oauth/x); exclua-o antes' } }])
      wrap()
      fireEvent.click(await screen.findByRole('button', { name: 'Excluir worktree' }))
      fireEvent.click(screen.getByRole('button', { name: 'Sim, excluir' }))
      expect(await screen.findByRole('alert')).toHaveTextContent('há outro worktree dentro deste (/w/api-oauth/x); exclua-o antes')
    })

    it('após excluir volta para a lista, mantendo o projeto, e avisa do branch mantido', async () => {
      mockDelete(free, [{ status: 200, body: { removed: true, branch_deleted: false, branch_kept_reason: 'não está integrado em main' } }])
      const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
      render(
        <QueryClientProvider client={qc}>
          <MemoryRouter initialEntries={['/worktrees/detalhe?project=1&path=%2Fw%2Fapi-oauth&projeto=1']}>
            <Routes>
              <Route path="/worktrees/detalhe" element={<WorktreeDetailPage />} />
              <Route path="/worktrees" element={<Worktrees />} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      )
      fireEvent.click(await screen.findByRole('button', { name: 'Excluir worktree' }))
      fireEvent.click(screen.getByRole('checkbox', { name: 'Também apagar o branch feat/oauth' }))
      fireEvent.click(screen.getByRole('button', { name: 'Sim, excluir' }))
      expect(await screen.findByRole('heading', { level: 1, name: 'Worktrees' })).toBeInTheDocument()
      expect(screen.getByText('Branch feat/oauth mantido: não está integrado em main.')).toBeInTheDocument()
    })

    it('com commits à frente da base, a caixa do branch continua liberada e só avisa', async () => {
      const calls = mockDelete({ ...free, git: { ...free.git!, ahead: 3 } }, [ok])
      wrap()
      fireEvent.click(await screen.findByRole('button', { name: 'Excluir worktree' }))
      const box = screen.getByRole('checkbox', { name: 'Também apagar o branch feat/oauth' })
      expect(box).toBeEnabled()
      expect(screen.getByText('(3 commits à frente de main; só será apagado se já estiver integrado na base do projeto)')).toBeInTheDocument()
      fireEvent.click(box)
      fireEvent.click(screen.getByRole('button', { name: 'Sim, excluir' }))
      await waitFor(() => expect(calls).toHaveLength(1))
      expect(calls[0].body).toEqual({ project: 1, path: '/w/api-oauth', force: false, delete_branch: true })
    })

    it('sem commits à frente da base, não mostra o aviso do branch', async () => {
      mockDelete(free, [])
      wrap()
      fireEvent.click(await screen.findByRole('button', { name: 'Excluir worktree' }))
      expect(screen.getByRole('checkbox', { name: 'Também apagar o branch feat/oauth' })).toBeEnabled()
      expect(screen.queryByText(/só será apagado se já estiver integrado/)).not.toBeInTheDocument()
    })

    it.each([
      ['processes_unknown', 'não consegui verificar os processos agora; confirme para excluir mesmo assim'],
      ['git_unknown', 'não consegui ler o estado do git agora; confirme para excluir mesmo assim'],
    ])('needs_confirm com %s mostra o motivo do servidor', async (flag, msg) => {
      mockDelete(free, [{ status: 409, body: { error: msg, needs_confirm: true, files: [], total_files: 0, processes: [], [flag]: true } }])
      wrap()
      fireEvent.click(await screen.findByRole('button', { name: 'Excluir worktree' }))
      fireEvent.click(screen.getByRole('button', { name: 'Sim, excluir' }))
      expect(await screen.findByText(msg)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Excluir mesmo assim' })).toBeInTheDocument()
    })

    function Probe() {
      const loc = useLocation()
      const nav = useNavigate()
      return (
        <>
          <span aria-label="estado">{JSON.stringify(loc.state)}</span>
          <button type="button" onClick={() => nav(-1)}>Voltar</button>
        </>
      )
    }
    // Do detalhe (vindo de /outra) exclui com o branch mantido e espera a lista.
    async function deleteAndLand(log: string[]) {
      mockDelete(free, [{ status: 200, body: { removed: true, branch_deleted: false, branch_kept_reason: 'não está integrado em main' } }], log)
      const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
      render(
        <QueryClientProvider client={qc}>
          <MemoryRouter initialEntries={['/outra', '/worktrees/detalhe?project=1&path=%2Fw%2Fapi-oauth&projeto=1']} initialIndex={1}>
            <Probe />
            <Routes>
              <Route path="/outra" element={<h1>Outra página</h1>} />
              <Route path="/worktrees/detalhe" element={<WorktreeDetailPage />} />
              <Route path="/worktrees" element={<Worktrees />} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      )
      fireEvent.click(await screen.findByRole('button', { name: 'Excluir worktree' }))
      fireEvent.click(screen.getByRole('checkbox', { name: 'Também apagar o branch feat/oauth' }))
      fireEvent.click(screen.getByRole('button', { name: 'Sim, excluir' }))
      expect(await screen.findByRole('heading', { level: 1, name: 'Worktrees' })).toBeInTheDocument()
    }

    it('após excluir não busca de novo o detalhe apagado', async () => {
      const log: string[] = []
      await deleteAndLand(log)
      await waitFor(() => expect(log).toContain('GET /api/worktrees'))
      const afterDelete = log.slice(log.findIndex((l) => l.startsWith('DELETE')) + 1)
      expect(afterDelete).not.toContain(`GET ${detailURL}`)
    })

    it('após excluir substitui o detalhe no histórico', async () => {
      await deleteAndLand([])
      fireEvent.click(screen.getByRole('button', { name: 'Voltar' }))
      expect(await screen.findByRole('heading', { level: 1, name: 'Outra página' })).toBeInTheDocument()
    })

    it('o aviso do branch mantido sai do estado do histórico, mas continua na tela', async () => {
      await deleteAndLand([])
      await waitFor(() => expect(screen.getByLabelText('estado')).toHaveTextContent('null'))
      expect(screen.getByText('Branch feat/oauth mantido: não está integrado em main.')).toBeInTheDocument()
      expect(screen.getByRole('heading', { level: 1, name: 'Worktrees' })).toBeInTheDocument()
    })
  })
})
