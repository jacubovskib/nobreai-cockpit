import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RtkCard } from './RtkCard'

function mock(routes: Record<string, unknown>) {
  const fn = vi.fn(async (url: string, init?: { method?: string; body?: string }) => {
    const m = init?.method ?? 'GET'
    const r = routes[`${m} ${url}`] ?? (m === 'GET' ? routes[url] : undefined)
    return r === undefined ? new Response('{}', { status: 404 }) : new Response(JSON.stringify(r), { status: 200 })
  })
  vi.stubGlobal('fetch', fn)
  return fn
}
const posts = (fn: ReturnType<typeof mock>) => fn.mock.calls.filter(([, i]) => i?.method === 'POST').map(([u, i]) => [u, JSON.parse(i?.body ?? '{}')])
function show() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}><RtkCard /></QueryClientProvider>)
}
afterEach(() => vi.unstubAllGlobals())

const base = { installed: true, path: '/bin/rtk', version: 'rtk 0.5.0', active: false, detail: '', activations: [] }

describe('RtkCard', () => {
  it('sem binário mostra o comando de instalação com copiar', async () => {
    mock({ '/api/rtk': { ...base, installed: false, path: '', version: '' } })
    show()
    expect(await screen.findByText('RTK não instalado')).toBeInTheDocument()
    expect(screen.getByText('cargo install --git https://github.com/rtk-ai/rtk --branch master rtk')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Copiar comando' })).toBeInTheDocument()
  })

  it('sem binário mostra o link do projeto e, sem hook, nenhum aviso', async () => {
    mock({ '/api/rtk': { ...base, installed: false, path: '', version: '' } })
    show()
    expect(await screen.findByRole('link', { name: 'https://github.com/rtk-ai/rtk' })).toHaveAttribute('href', 'https://github.com/rtk-ai/rtk')
    expect(screen.queryByText(/binário sumiu/)).not.toBeInTheDocument()
  })

  it('hook ainda no settings sem binário avisa', async () => {
    mock({ '/api/rtk': { ...base, installed: false, active: true, path: '', version: '' } })
    show()
    expect(await screen.findByText('O hook do RTK continua no settings.json, mas o binário sumiu: reinstale ou remova o hook.')).toBeInTheDocument()
  })

  it('ativa só depois de confirmar a prévia', async () => {
    const fn = mock({
      '/api/rtk': base,
      '/api/rtk/preview?action=activate': { output: 'vai adicionar o hook' },
      'POST /api/rtk': { output: 'feito', backup: '/tmp/settings.bak', status: { ...base, active: true } },
    })
    show()
    expect(await screen.findByText(/rtk 0\.5\.0/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Ativar no Claude Code' }))
    expect(await screen.findByText('vai adicionar o hook')).toBeInTheDocument()
    expect(posts(fn)).toHaveLength(0)
    fireEvent.click(screen.getByRole('button', { name: 'Confirmar' }))
    await waitFor(() => expect(posts(fn)).toEqual([['/api/rtk', { action: 'activate' }]]))
    expect(await screen.findByText('feito')).toBeInTheDocument()
    expect(screen.getByText(/Cópia do settings em/)).toHaveTextContent('/tmp/settings.bak')
  })

  it('cancelar não faz POST', async () => {
    const fn = mock({ '/api/rtk': base, '/api/rtk/preview?action=activate': { output: 'prévia' } })
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Ativar no Claude Code' }))
    await screen.findByText('prévia')
    fireEvent.click(screen.getByRole('button', { name: 'Cancelar' }))
    expect(screen.queryByText('prévia')).not.toBeInTheDocument()
    expect(posts(fn)).toHaveLength(0)
  })

  it('ativo mostra Ativo e Desativar com o mesmo fluxo', async () => {
    const fn = mock({
      '/api/rtk': { ...base, active: true },
      '/api/rtk/preview?action=deactivate': { output: 'vai remover' },
      'POST /api/rtk': { output: 'removido', backup: '/b', status: base },
    })
    show()
    expect(await screen.findByText('Ativo')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Desativar' }))
    await screen.findByText('vai remover')
    fireEvent.click(screen.getByRole('button', { name: 'Confirmar' }))
    await waitFor(() => expect(posts(fn)).toEqual([['/api/rtk', { action: 'deactivate' }]]))
  })
})
