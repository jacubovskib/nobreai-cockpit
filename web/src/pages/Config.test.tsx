import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Config } from './Config'

type Reply = unknown
function mock(routes: Record<string, Reply>) {
  const fn = vi.fn(async (url: string, init?: { method?: string; body?: string }) => {
    const m = init?.method ?? 'GET'
    const r = routes[`${m} ${url}`] ?? (m === 'GET' ? routes[url] : undefined)
    if (r === undefined) return new Response('{}', { status: 404 })
    if (r && typeof r === 'object' && 'status' in r && 'body' in r) {
      const o = r as { status: number; body: unknown }
      return new Response(JSON.stringify(o.body), { status: o.status })
    }
    return new Response(JSON.stringify(r), { status: 200 })
  })
  vi.stubGlobal('fetch', fn)
  return fn
}
const calls = (fn: ReturnType<typeof mock>, method: string, url: string) =>
  fn.mock.calls.filter(([u, i]) => u === url && (i?.method ?? 'GET') === method).map(([, i]) => JSON.parse(i?.body ?? '{}'))
function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}><MemoryRouter>{ui}</MemoryRouter></QueryClientProvider>)
}
afterEach(() => vi.unstubAllGlobals())

const settings = {
  rules: { goal: true, where_stopped: true, essential_file: true, todo_open: true, how_to_test: true, no_open_questions: false },
  where_stopped_ttl_min: 15, grace_min: 15, reminder_edit_threshold: 3, launch_mode: 'terminal', terminal_cmd: ['x-terminal-emulator', '-e'],
  currency: 'USD', brl_rate: 0, incomplete_action: 'ask_agent', demo: false,
  agent_creates_cards: true, inject_paused_cards: true, memory_budget_tokens: 1500,
}
const routes = (s: object = settings) => ({
  '/api/settings': s, '/api/limits': [], '/api/projects': [], '/api/cards': [], '/api/prices': [],
  '/api/usage/totals?group=model': [], 'PUT /api/settings': s,
})

describe('Config opções reais', () => {
  it('salva as opções de criação de card', async () => {
    const fn = mock(routes())
    wrap(<Config />)
    const sec = await screen.findByRole('region', { name: 'Criação de cards' })
    expect(within(sec).queryByText('em breve')).not.toBeInTheDocument()
    const agent = within(sec).getByLabelText(/Quando o agente identificar/)
    expect(agent).toBeEnabled()
    expect(agent).toBeChecked()
    const paused = within(sec).getByLabelText(/Mostrar cards pausados/)
    expect(paused).toBeEnabled()
    fireEvent.click(paused)
    fireEvent.click(within(sec).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(calls(fn, 'PUT', '/api/settings')).toHaveLength(1))
    expect(calls(fn, 'PUT', '/api/settings')[0]).toMatchObject({ agent_creates_cards: true, inject_paused_cards: false, reminder_edit_threshold: 3 })
  })

  it('salva o limite das memórias', async () => {
    const fn = mock(routes())
    wrap(<Config />)
    const sec = await screen.findByRole('region', { name: 'Criação de cards' })
    const f = within(sec).getByLabelText(/Limite das memórias por sessão/)
    expect(f).toHaveValue('1500')
    fireEvent.change(f, { target: { value: '0' } })
    fireEvent.click(within(sec).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(calls(fn, 'PUT', '/api/settings')).toHaveLength(1))
    expect(calls(fn, 'PUT', '/api/settings')[0]).toMatchObject({ memory_budget_tokens: 0 })
    fireEvent.change(f, { target: { value: '2001' } })
    fireEvent.click(within(sec).getByRole('button', { name: 'Salvar' }))
    expect(await within(sec).findByText('O limite das memórias deve ficar entre 0 e 2000 tokens.')).toBeInTheDocument()
    expect(calls(fn, 'PUT', '/api/settings')).toHaveLength(1)
  })

  it('salva carência e comando do terminal', async () => {
    const fn = mock(routes())
    wrap(<Config />)
    const sec = await screen.findByRole('region', { name: /pronto para handoff/ })
    expect(within(sec).getByLabelText('Comando do terminal (separado por espaços)')).toHaveValue('x-terminal-emulator -e')
    fireEvent.change(within(sec).getByLabelText('Carência antes de encerrar uma sessão cujo processo sumiu (minutos)'), { target: { value: '20' } })
    fireEvent.change(within(sec).getByLabelText('Comando do terminal (separado por espaços)'), { target: { value: ' kitty   -e ' } })
    fireEvent.click(within(sec).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(calls(fn, 'PUT', '/api/settings')).toHaveLength(1))
    expect(calls(fn, 'PUT', '/api/settings')[0]).toMatchObject({ grace_min: 20, terminal_cmd: ['kitty', '-e'] })
  })

  it('esconde o comando do terminal fora do modo terminal', async () => {
    mock(routes({ ...settings, launch_mode: 'background' }))
    wrap(<Config />)
    const sec = await screen.findByRole('region', { name: /pronto para handoff/ })
    expect(within(sec).queryByLabelText('Comando do terminal (separado por espaços)')).not.toBeInTheDocument()
  })

  it('carência inválida não salva', async () => {
    const fn = mock(routes())
    wrap(<Config />)
    const sec = await screen.findByRole('region', { name: /pronto para handoff/ })
    fireEvent.change(within(sec).getByLabelText('Carência antes de encerrar uma sessão cujo processo sumiu (minutos)'), { target: { value: '-1' } })
    fireEvent.click(within(sec).getByRole('button', { name: 'Salvar' }))
    expect(await within(sec).findByRole('alert')).toBeInTheDocument()
    expect(calls(fn, 'PUT', '/api/settings')).toHaveLength(0)
  })

  it('mostra o erro do daemon ao salvar o handoff', async () => {
    mock({ ...routes(), 'PUT /api/settings': { status: 400, body: { error: 'comando do terminal inválido' } } })
    wrap(<Config />)
    const sec = await screen.findByRole('region', { name: /pronto para handoff/ })
    fireEvent.click(within(sec).getByRole('button', { name: 'Salvar' }))
    expect(await within(sec).findByRole('alert')).toHaveTextContent('comando do terminal inválido')
  })

  it('usa os textos novos das opções mark e pause', async () => {
    mock(routes())
    wrap(<Config />)
    expect(await screen.findByRole('option', { name: 'Marcar o card como “handoff incompleto” e avisar na Sala de controle' })).toBeInTheDocument()
  })
})

describe('Config sub-projetos', () => {
  const loja = { id: 1, name: 'loja', path: '/w/loja', capture: true, base_branch: 'main', parent_id: null }
  const web = { id: 2, name: 'web', path: '/w/loja/web', capture: false, base_branch: 'main', parent_id: null }
  const api = { id: 3, name: 'api', path: '/w/loja/api', capture: true, base_branch: 'main', parent_id: 1 }

  it('move to parent warns about capture', async () => {
    const fn = mock({ ...routes(), '/api/projects': [loja, web], '/api/projects/1/suggestions': [], 'PATCH /api/projects/2': { ...web, parent_id: 1, moved: { sessions: 2, cards: 1 } } })
    wrap(<Config />)
    const sec = await screen.findByRole('region', { name: 'Projetos' })
    fireEvent.change(await within(sec).findByLabelText('Mover web para…'), { target: { value: '1' } })
    expect(within(sec).getByText('A captura de web passa a seguir a de loja (ligada).')).toBeInTheDocument()
    fireEvent.click(within(sec).getByRole('button', { name: 'Mover para loja' }))
    expect(await within(sec).findByText('2 sessões e 1 card passaram para web.')).toBeInTheDocument()
    expect(calls(fn, 'PATCH', '/api/projects/2')).toEqual([{ parent_id: 1 }])
  })

  it('move report survives the refetch that remounts the row', async () => {
    const routesMap: Record<string, unknown> = { ...routes(), '/api/projects': [loja, web], '/api/projects/1/suggestions': [], 'PATCH /api/projects/2': { ...web, parent_id: 1, moved: { sessions: 2, cards: 1 } } }
    mock(routesMap)
    wrap(<Config />)
    const sec = await screen.findByRole('region', { name: 'Projetos' })
    fireEvent.change(await within(sec).findByLabelText('Mover web para…'), { target: { value: '1' } })
    routesMap['/api/projects'] = [loja, { ...web, parent_id: 1 }]
    fireEvent.click(within(sec).getByRole('button', { name: 'Mover para loja' }))
    // Espera a linha remontar como filha (o botão de desligar só existe nela).
    await screen.findByRole('button', { name: 'Desligar do projeto principal' })
    expect(screen.getByText('2 sessões e 1 card passaram para web.')).toBeInTheDocument()
  })

  it('adds selected suggestions as children', async () => {
    const sug = [{ path: '/w/loja/a', rel: 'a', name: 'a', source: 'pnpm' }, { path: '/w/loja/b', rel: 'b', name: 'b', source: 'pnpm' }, { path: '/w/loja/c', rel: 'c', name: 'c', source: 'pnpm' }]
    const fn = mock({ ...routes(), '/api/projects': [loja], '/api/projects/1/suggestions': sug, 'POST /api/projects': loja })
    wrap(<Config />)
    expect(await screen.findByText('3 sub-projetos sugeridos')).toBeInTheDocument()
    fireEvent.click(screen.getByLabelText(/^c /))
    fireEvent.click(screen.getByRole('button', { name: 'Adicionar selecionados' }))
    await waitFor(() => expect(calls(fn, 'POST', '/api/projects')).toHaveLength(2))
    expect(calls(fn, 'POST', '/api/projects')).toEqual([
      { name: 'a', path: '/w/loja/a', parent_id: 1 }, { name: 'b', path: '/w/loja/b', parent_id: 1 },
    ])
  })

  it('dismiss suggestions calls dismiss route', async () => {
    const sug = [{ path: '/w/loja/a', rel: 'a', name: 'a', source: 'pnpm' }]
    const fn = mock({ ...routes(), '/api/projects': [loja], '/api/projects/1/suggestions': sug, 'POST /api/projects/1/suggestions/dismiss': {} })
    wrap(<Config />)
    fireEvent.click(await screen.findByRole('button', { name: 'Não sugerir estes' }))
    await waitFor(() => expect(calls(fn, 'POST', '/api/projects/1/suggestions/dismiss')).toEqual([{ rels: ['a'] }]))
  })

  it('capture toggle hidden on child', async () => {
    mock({ ...routes(), '/api/projects': [loja, api], '/api/projects/1/suggestions': [] })
    wrap(<Config />)
    await screen.findByText('api')
    expect(screen.getByLabelText('Capturar sessões de loja')).toBeInTheDocument()
    expect(screen.queryByLabelText('Capturar sessões de api')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Desligar do projeto principal' })).toBeInTheDocument()
  })
})

describe('Config sessões avisadas e histórico', () => {
  const save = async (reply: object) => {
    mock({ ...routes(), 'PUT /api/settings': reply, '/api/config-log': [] })
    wrap(<Config />)
    const sec = await screen.findByRole('region', { name: 'Criação de cards' })
    fireEvent.click(within(sec).getByRole('button', { name: 'Salvar' }))
    await within(sec).findByText('Salvo.')
    return sec
  }
  it('shows notified sessions after save', async () => {
    const sec = await save({ ...settings, notified_sessions: 2 })
    expect(within(sec).getByText('2 sessões abertas vão receber o aviso no próximo passo.')).toBeInTheDocument()
  })
  it('zero notified shows only Salvo', async () => {
    const sec = await save({ ...settings, notified_sessions: 0 })
    expect(within(sec).queryByText(/receber o aviso/)).not.toBeInTheDocument()
  })
  it('config log lists entries', async () => {
    mock({ ...routes(), '/api/config-log?limit=50': [{ id: 1, at: '2026-11-17T10:30:00Z', kind: 'integration_fixed', data: { keys: ['hooks'], changed: [] } }] })
    wrap(<Config />)
    expect(await screen.findByText(/hooks corrigido por você/)).toBeInTheDocument()
  })
  it('config log shortens a prefixed session id', async () => {
    mock({ ...routes(), '/api/config-log?limit=50': [{ id: 2, at: '2026-11-17T10:30:00Z', kind: 'agent_notified', data: { session: 'claude:abcdef12-3456-7890-abcd-ef1234567890', text: 'x' } }] })
    wrap(<Config />)
    expect(await screen.findByText(/Sessão abcdef12 avisada/)).toBeInTheDocument()
  })
  it('capture toggle shows notified sessions', async () => {
    const loja = { id: 1, name: 'loja', path: '/w/loja', capture: true, base_branch: 'main', parent_id: null }
    const fn = mock({ ...routes(), '/api/projects': [loja], '/api/projects/1/suggestions': [], 'PATCH /api/projects/1': { ...loja, capture: false, notified_sessions: 2 } })
    wrap(<Config />)
    const sec = await screen.findByRole('region', { name: 'Projetos' })
    fireEvent.click(await within(sec).findByLabelText('Capturar sessões de loja'))
    expect(await within(sec).findByText('2 sessões abertas vão receber o aviso no próximo passo.')).toBeInTheDocument()
    expect(calls(fn, 'PATCH', '/api/projects/1')).toEqual([{ capture: false }])
  })
  it('config log names the RTK on and off', async () => {
    mock({ ...routes(), '/api/config-log?limit=50': [
      { id: 3, at: '2026-11-17T10:30:00Z', kind: 'rtk_on', data: { version: '1.2.0', backup: '/b' } },
      { id: 4, at: '2026-11-17T10:40:00Z', kind: 'rtk_off', data: { version: '1.2.0', backup: '/b' } },
    ] })
    wrap(<Config />)
    expect(await screen.findByText(/RTK ativado no Claude Code/)).toBeInTheDocument()
    expect(screen.getByText(/RTK desativado/)).toBeInTheDocument()
  })
  it('config log empty', async () => {
    mock({ ...routes(), '/api/config-log?limit=50': [] })
    wrap(<Config />)
    expect(await screen.findByText('Nenhuma mudança registrada.')).toBeInTheDocument()
  })
})

describe('Config monitor de contexto', () => {
  const withCtx = { ...settings, context_warn_pcts: [60, 80], big_output_tokens: 15000, big_output_max: 3, agent_context_warnings: true, model_windows: { 'claude-opus-5-5': 1000000 } }

  it('salva as faixas, a saída grande, o máximo e o aviso', async () => {
    const fn = mock(routes(withCtx))
    wrap(<Config />)
    const sec = await screen.findByRole('region', { name: 'Monitor de contexto' })
    expect(within(sec).getByLabelText('Faixa 1 (% da janela)')).toHaveValue('60')
    expect(within(sec).getByLabelText('Avisar o agente')).toBeChecked()
    fireEvent.change(within(sec).getByLabelText('Faixa 1 (% da janela)'), { target: { value: '50' } })
    fireEvent.change(within(sec).getByLabelText('Saída grande (tokens)'), { target: { value: '20000' } })
    fireEvent.change(within(sec).getByLabelText(/Máximo de avisos/), { target: { value: '5' } })
    fireEvent.click(within(sec).getByLabelText('Avisar o agente'))
    fireEvent.click(within(sec).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(calls(fn, 'PUT', '/api/settings')).toHaveLength(1))
    expect(calls(fn, 'PUT', '/api/settings')[0]).toMatchObject({ context_warn_pcts: [50, 80], big_output_tokens: 20000, big_output_max: 5, agent_context_warnings: false })
  })

  it('lista vazia salva e avisa que não há avisos de faixa', async () => {
    const fn = mock(routes({ ...withCtx, context_warn_pcts: [] }))
    wrap(<Config />)
    const sec = await screen.findByRole('region', { name: 'Monitor de contexto' })
    expect(within(sec).getByText(/sem avisos de faixa/i)).toBeInTheDocument()
    expect(within(sec).queryByLabelText(/^Faixa 1/)).toBeNull()
    fireEvent.click(within(sec).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(calls(fn, 'PUT', '/api/settings')).toHaveLength(1))
    expect(calls(fn, 'PUT', '/api/settings')[0]).toMatchObject({ context_warn_pcts: [] })
  })

  it('mostra e salva três faixas inteiras, e remove todas', async () => {
    const fn = mock(routes({ ...withCtx, context_warn_pcts: [50, 70, 90] }))
    wrap(<Config />)
    const sec = await screen.findByRole('region', { name: 'Monitor de contexto' })
    expect(within(sec).getByLabelText('Faixa 3 (% da janela)')).toHaveValue('90')
    expect(within(sec).queryByText(/sem avisos de faixa/i)).toBeNull()
    fireEvent.click(within(sec).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(calls(fn, 'PUT', '/api/settings')).toHaveLength(1))
    expect(calls(fn, 'PUT', '/api/settings')[0]).toMatchObject({ context_warn_pcts: [50, 70, 90] })
    for (let i = 0; i < 3; i++) fireEvent.click(within(sec).getAllByRole('button', { name: /^Remover faixa/ })[0])
    expect(within(sec).getByText(/sem avisos de faixa/i)).toBeInTheDocument()
  })

  it('acrescenta uma faixa', async () => {
    const fn = mock(routes(withCtx))
    wrap(<Config />)
    const sec = await screen.findByRole('region', { name: 'Monitor de contexto' })
    fireEvent.click(within(sec).getByRole('button', { name: 'Adicionar faixa' }))
    fireEvent.change(within(sec).getByLabelText('Faixa 3 (% da janela)'), { target: { value: '95' } })
    fireEvent.click(within(sec).getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(calls(fn, 'PUT', '/api/settings')).toHaveLength(1))
    expect(calls(fn, 'PUT', '/api/settings')[0]).toMatchObject({ context_warn_pcts: [60, 80, 95] })
  })

  it.each([
    ['Faixa 1 (% da janela)', '80', /em ordem crescente/],
    ['Faixa 1 (% da janela)', '0', /entre 1 e 99/],
    ['Faixa 1 (% da janela)', '', /entre 1 e 99/],
    ['Faixa 2 (% da janela)', '100', /entre 1 e 99/],
    ['Saída grande (tokens)', '999', /pelo menos 1000/],
    [/Máximo de avisos/, '21', /entre 0 e 20/],
  ])('valida %s=%s sem enviar', async (label, value, msg) => {
    const fn = mock(routes(withCtx))
    wrap(<Config />)
    const sec = await screen.findByRole('region', { name: 'Monitor de contexto' })
    fireEvent.change(within(sec).getByLabelText(label), { target: { value } })
    fireEvent.click(within(sec).getByRole('button', { name: 'Salvar' }))
    expect(await within(sec).findByText(msg)).toBeInTheDocument()
    expect(calls(fn, 'PUT', '/api/settings')).toHaveLength(0)
  })

  it('edita a tabela de janelas por modelo: adiciona, remove e valida', async () => {
    const fn = mock(routes(withCtx))
    wrap(<Config />)
    const sec = await screen.findByRole('region', { name: 'Preços e moeda' })
    expect(within(sec).getByDisplayValue('claude-opus-5-5')).toBeInTheDocument()
    fireEvent.click(within(sec).getByRole('button', { name: 'Adicionar janela' }))
    const models = within(sec).getAllByLabelText('Modelo')
    fireEvent.change(models[models.length - 1], { target: { value: 'outro' } })
    const wins = within(sec).getAllByLabelText('Janela (tokens)')
    fireEvent.change(wins[wins.length - 1], { target: { value: '500' } })
    fireEvent.click(within(sec).getByRole('button', { name: 'Salvar janelas' }))
    expect(await within(sec).findByText(/pelo menos 1000/)).toBeInTheDocument()
    expect(calls(fn, 'PUT', '/api/settings')).toHaveLength(0)
    fireEvent.change(wins[wins.length - 1], { target: { value: '128.000' } })
    fireEvent.click(within(sec).getByRole('button', { name: 'Remover janela de claude-opus-5-5' }))
    fireEvent.click(within(sec).getByRole('button', { name: 'Salvar janelas' }))
    await waitFor(() => expect(calls(fn, 'PUT', '/api/settings')).toHaveLength(1))
    expect(calls(fn, 'PUT', '/api/settings')[0].model_windows).toEqual({ outro: 128000 })
  })
})
