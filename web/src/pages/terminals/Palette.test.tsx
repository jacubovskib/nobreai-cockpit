import '@testing-library/jest-dom/vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Palette } from './Palette'
import { registerInput, type TileInputHandle } from './inputRegistry'

const json = (v: unknown) => new Response(JSON.stringify(v), { status: 200 })
const PROJECT = { id: 1, name: 'loja', path: '/w/loja', capture: true, base_branch: 'main', parent_id: null, kind: 'repo' as const }
const prompt = (o: object) => ({ id: 1, name: 'Revisar', slug: 'revisar', tag: '', description: '', body: '', agents: ['claude'], project_id: null, is_default_handoff: false, uses: 0, last_used_at: null, ...o })
const card = (o: object) => ({ id: 1, key: 'NAC-7', title: 'Título', where_stopped: 'x', column: 'em_andamento', updated_at: '2026-11-20T10:00:00Z', ...o })
const mem = (o: object) => ({ id: 1, project_id: null, rule: 'regra', do: 'fazer', status: 'ativa', ...o })

function mount(over: Partial<React.ComponentProps<typeof Palette>> = {}) {
  const onClose = vi.fn()
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  mounted = render(
    <QueryClientProvider client={qc}>
      <Palette targetId="t1" targetLabel="Claude Code · NAC-63" agent="claude" project={PROJECT} onClose={onClose} {...over} />
    </QueryClientProvider>,
  )
  return onClose
}

let mounted: ReturnType<typeof render> | null = null
const handle = (): TileInputHandle => ({ insert: vi.fn(), pickPrompt: vi.fn(), focus: vi.fn() })
let off: () => void = () => {}
let h: TileInputHandle

beforeEach(() => {
  h = handle()
  off = registerInput('t1', h)
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (url === '/api/prompts') return json([prompt({}), prompt({ id: 2, name: 'Outro agente', slug: 'kiro-only', agents: ['kiro'] })])
    if (url === '/api/cards') return json([card({}), card({ id: 2, key: 'NAC-8', title: 'Pronto', column: 'concluido' })])
    if (url.startsWith('/api/memories')) return json([mem({})])
    return json([])
  }))
})
afterEach(() => { off(); vi.unstubAllGlobals() })

describe('Palette', () => {
  it('abre com a busca em foco e o alvo no cabeçalho', async () => {
    mount()
    expect(screen.getByRole('dialog', { name: 'Buscar' })).toHaveAttribute('aria-modal', 'true')
    expect(screen.getByPlaceholderText('Prompt salvo, card ou memória')).toHaveFocus()
    expect(screen.getByText('Inserir em: Claude Code · NAC-63')).toBeInTheDocument()
    await screen.findByText('NAC-7 · Título')
  })

  it('escolher um card insere o texto na barra e fecha', async () => {
    const onClose = mount()
    fireEvent.click(await screen.findByText('NAC-7 · Título'))
    expect(h.insert).toHaveBeenCalledWith('NAC-7 · Título\nOnde parei: x')
    expect(onClose).toHaveBeenCalled()
  })

  it('card concluído não aparece', async () => {
    mount()
    await screen.findByText('NAC-7 · Título')
    expect(screen.queryByText(/NAC-8/)).toBeNull()
  })

  it('escolher um prompt chama pickPrompt com o slug; prompt de outro agente não aparece', async () => {
    mount()
    fireEvent.click(await screen.findByText('Revisar'))
    expect(h.pickPrompt).toHaveBeenCalledWith('revisar')
    expect(screen.queryByText('Outro agente')).toBeNull()
  })

  it('memória só aparece com busca e insere regra — fazer', async () => {
    mount()
    await screen.findByText('Revisar')
    expect(screen.queryByText('Memórias')).toBeNull()
    fireEvent.change(screen.getByLabelText('Buscar', { selector: 'input' }), { target: { value: 'regra' } })
    fireEvent.click(await screen.findByText('regra — fazer'))
    expect(h.insert).toHaveBeenCalledWith('regra — fazer')
  })

  it('sem handle registrado os itens ficam desabilitados e aparece o aviso', async () => {
    off()
    mount()
    expect(screen.getByText('Clique num terminal para escolher onde inserir.')).toBeInTheDocument()
    const item = await screen.findByText('NAC-7 · Título')
    expect(item.closest('button')).toBeDisabled()
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Enter' })
    expect(h.insert).not.toHaveBeenCalled()
  })

  it('sem alvo lista todos os prompts, só filtrados pela busca', async () => {
    mount({ targetId: null, targetLabel: '', agent: '', project: null })
    expect(await screen.findByText('Outro agente')).toBeInTheDocument()
    expect(screen.getByText('Revisar')).toBeInTheDocument()
  })

  it('setas percorrem os itens e Enter escolhe o marcado', async () => {
    mount()
    await screen.findByText('NAC-7 · Título')
    const dlg = screen.getByRole('dialog')
    fireEvent.keyDown(dlg, { key: 'ArrowDown' })
    fireEvent.keyDown(dlg, { key: 'Enter' })
    expect(h.insert).toHaveBeenCalledWith('NAC-7 · Título\nOnde parei: x')
    expect(h.pickPrompt).not.toHaveBeenCalled()
  })

  it('Enter sem mexer nas setas escolhe o primeiro (o prompt)', async () => {
    mount()
    await screen.findByText('NAC-7 · Título')
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Enter' })
    expect(h.pickPrompt).toHaveBeenCalledWith('revisar')
  })

  it('Esc e clique fora chamam onClose', async () => {
    const onClose = mount()
    await screen.findByText('Revisar')
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
    fireEvent.mouseDown(screen.getByRole('dialog').parentElement!)
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  it('clique num item desligado leva o foco ao body e Esc ainda fecha', async () => {
    off()
    const onClose = mount()
    const item = (await screen.findByText('NAC-7 · Título')).closest('button')!
    fireEvent.mouseDown(item)
    ;(document.activeElement as HTMLElement).blur()
    expect(document.activeElement).toBe(document.body)
    fireEvent.keyDown(document.body, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('com o foco no body (clique no título do grupo), setas e Enter ainda escolhem', async () => {
    mount()
    await screen.findByText('NAC-7 · Título')
    fireEvent.mouseDown(screen.getByText('Cards'))
    ;(document.activeElement as HTMLElement).blur()
    fireEvent.keyDown(document.body, { key: 'ArrowDown' })
    fireEvent.keyDown(document.body, { key: 'Enter' })
    expect(h.insert).toHaveBeenCalledWith('NAC-7 · Título\nOnde parei: x')
    expect(h.insert).toHaveBeenCalledTimes(1)
  })

  it('fechada (desmontada), Esc não chama mais onClose', async () => {
    const onClose = mount()
    await screen.findByText('Revisar')
    mounted!.unmount()
    fireEvent.keyDown(document.body, { key: 'Escape' })
    expect(onClose).not.toHaveBeenCalled()
  })

  it('nada encontrado', async () => {
    mount()
    await screen.findByText('Revisar')
    fireEvent.change(screen.getByLabelText('Buscar', { selector: 'input' }), { target: { value: 'zzzz' } })
    await waitFor(() => expect(screen.getByText('Nada encontrado.')).toBeInTheDocument())
  })
})
