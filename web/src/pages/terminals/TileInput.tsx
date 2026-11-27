import { memo, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { ApiError, post } from '../../api/client'
import { usePrompts } from '../../api/queries'
import type { Project } from '../../api/types'
import { commandWarning, inputError, insertAt, promptsFor, sendInput } from './inputModel'
import { registerInput } from './inputRegistry'

// Barra de entrada do tile: o texto digitado vive aqui, em estado local, para
// que digitar não renderize o tile nem a página.

export interface TileInputProps {
  terminalId: string; agent: string; agentName: string
  cardKey: string; project: Project | null
  run: boolean        // terminal de `nobre run`
  simple: boolean
}

const MAX_ROWS = 6
const btn: React.CSSProperties = { display: 'inline-flex', alignItems: 'center', justifyContent: 'center', minHeight: 28, padding: '0 10px', borderRadius: 6, border: '1px solid #3A3F4B', background: 'transparent', color: 'var(--text)', font: 'inherit', cursor: 'pointer' }
const footer: React.CSSProperties = { display: 'flex', flexDirection: 'column', gap: 4, padding: '6px 30px 6px 8px', borderTop: '1px solid var(--border)', fontSize: 13 }

const firstLine = (s: string) => s.split('\n')[0]

// Texto da barra vindo de um prompt: o aviso de card e de comando.
interface Preview { noCard: boolean; command: boolean }

function TileInputView(p: TileInputProps) {
  const [freed, setFreed] = useState(false)
  const [text, setText] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [closed, setClosed] = useState(false)   // lista do # fechada por Esc
  const [sel, setSel] = useState(0)
  const [pickError, setPickError] = useState('')
  const [preview, setPreview] = useState<Preview | null>(null)
  const prompts = usePrompts()
  const slug = useRef('')   // prompt escolhido, mandado junto com o envio
  const area = useRef<HTMLTextAreaElement>(null)
  const textRef = useRef(text)
  textRef.current = text
  const caret = useRef<number | null>(null) // cursor a aplicar depois do próximo render
  const busy = useRef(false)
  const pickSeq = useRef(0)
  const pickRef = useRef<(slug: string) => void>(() => {})
  const bar = !p.run || freed

  // Registro para a paleta e a lista do #: só enquanto a barra existe.
  useEffect(() => {
    if (!bar) return
    const focus = () => area.current?.focus()
    return registerInput(p.terminalId, {
      insert(piece) {
        const el = area.current
        const cur = textRef.current
        const start = el?.selectionStart ?? cur.length
        const end = el?.selectionEnd ?? cur.length
        const r = insertAt(cur, start, end, piece)
        caret.current = r.caret
        setText(r.text)
        setError('')
        focus()
      },
      pickPrompt: (s) => pickRef.current(s),
      focus,
    })
  }, [p.terminalId, bar])

  useLayoutEffect(() => {
    if (caret.current === null) return
    area.current?.setSelectionRange(caret.current, caret.current)
    caret.current = null
  }, [text])

  // Renderiza o prompt para o card do tile e troca o texto da barra.
  const pick = async (s: string) => {
    const seq = ++pickSeq.current
    setPickError('')
    let noCard = p.cardKey === ''
    try {
      let r: { text: string }
      try {
        r = await post<{ text: string }>('/api/prompts/' + s + '/render', { card: p.cardKey })
      } catch (e) {
        if (!(e instanceof ApiError && e.status === 404 && p.cardKey !== '')) throw e
        noCard = true
        r = await post<{ text: string }>('/api/prompts/' + s + '/render', { card: '' })
      }
      if (seq !== pickSeq.current) return
      slug.current = s
      caret.current = r.text.length
      setText(r.text)
      setClosed(true)
      setSel(0)
      setError('')
      setPreview({ noCard, command: commandWarning(r.text) })
      area.current?.focus()
    } catch (e) {
      if (seq !== pickSeq.current) return
      const unknown = e instanceof ApiError && e.status === 422 ? (e.body as { unknown?: string[] } | null)?.unknown : undefined
      setPickError(unknown?.length ? 'Variável desconhecida: ' + unknown.map((v) => '{{' + v + '}}').join(', ') : 'Não foi possível carregar o prompt.')
    }
  }
  pickRef.current = (s) => { void pick(s) }

  const clear = () => {
    slug.current = ''
    setText('')
    setPreview(null)
    setPickError('')
    area.current?.focus()
  }

  const send = async () => {
    const t = textRef.current
    if (busy.current || t.trim() === '') return
    busy.current = true
    setPending(true)
    try {
      await sendInput(p.terminalId, t, slug.current)
      slug.current = ''
      setPreview(null)
      // Só esvazia se nada foi digitado enquanto o envio estava pendente.
      if (textRef.current === t) setText('')
      setError('')
      area.current?.focus()
    } catch (e) {
      setError(inputError(e))
    } finally {
      busy.current = false
      setPending(false)
    }
  }

  if (!bar) {
    return (
      <div data-testid="tile-input" style={{ ...footer, flexDirection: 'row', alignItems: 'center', flexWrap: 'wrap', gap: 8 }}>
        <span style={{ color: 'var(--text-2)' }}>Sessão do nobre run: você só assiste.</span>
        <button type="button" style={btn} onClick={() => setFreed(true)}>Liberar entrada</button>
      </div>
    )
  }

  const open = text.startsWith('#') && !closed
  const found = open ? promptsFor(prompts.data ?? [], p.agent, p.project, text.slice(1)) : []
  const rows = Math.min(MAX_ROWS, Math.max(1, text.split('\n').length))
  return (
    <div data-testid="tile-input" style={footer}>
      {open && (
        <div>
          <ul role="listbox" aria-label="Prompts salvos" style={{ listStyle: 'none', margin: 0, padding: 0, maxHeight: 160, overflowY: 'auto', border: '1px solid #3A3F4B', borderRadius: 6 }}>
            {found.map((x, i) => (
              <li key={x.id} role="option" aria-selected={i === sel} onMouseDown={(e) => e.preventDefault()} onClick={() => void pick(x.slug)}
                style={{ padding: '4px 8px', cursor: 'pointer', background: i === sel ? 'var(--bg-2, #2A2F3A)' : 'transparent' }}>
                <strong>{x.name}</strong>{' '}
                <span style={{ color: 'var(--text-2)' }}>{firstLine(x.description || x.body)}</span>
              </li>
            ))}
          </ul>
          {found.length === 0 && <p style={{ margin: '4px 0 0', color: 'var(--text-2)' }}>Nenhum prompt com “{text.slice(1)}”.</p>}
          {pickError && <p role="alert" style={{ margin: '4px 0 0', fontSize: 12, color: 'var(--err)' }}>{pickError}</p>}
        </div>
      )}
      <div style={{ display: 'flex', gap: 6, alignItems: 'flex-end' }}>
        <textarea
          ref={area}
          aria-label={'Enviar ao agente ' + p.agentName}
          placeholder="Enviar ao agente…  # prompts salvos"
          rows={rows}
          value={text}
          onChange={(e) => {
            const v = e.target.value
            setText(v)
            setError('')
            setPickError('')
            setSel(0)
            if (!v.startsWith('#')) setClosed(false)
            if (v === '') { slug.current = ''; setPreview(null) }
          }}
          onKeyDown={(e) => {
            if (e.nativeEvent.isComposing) return
            if (open) {
              if (e.key === 'Escape') { e.preventDefault(); setClosed(true); return }
              if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
                e.preventDefault()
                if (found.length) setSel((i) => (i + (e.key === 'ArrowDown' ? 1 : found.length - 1)) % found.length)
                return
              }
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                if (found[sel]) void pick(found[sel].slug)
                return
              }
            }
            if (e.key !== 'Enter' || e.shiftKey) return
            e.preventDefault()
            void send()
          }}
          style={{ flex: 1, minWidth: 0, resize: 'none', padding: '4px 8px', borderRadius: 6, border: '1px solid #3A3F4B', background: 'var(--bg)', color: 'var(--text)', font: 'inherit' }}
        />
        <button type="button" style={btn} disabled={pending || text.trim() === ''} onClick={() => void send()}>Enviar</button>
      </div>
      {preview && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 2, fontSize: 12, color: 'var(--text-2)' }}>
          <span>Prévia: o agente vai receber este texto. Edite antes de enviar. <button type="button" style={{ ...btn, minHeight: 22, padding: '0 6px' }} onClick={clear}>Limpar</button></span>
          {preview.noCard && <span>Sem card: as variáveis do card ficaram vazias.</span>}
          {preview.command && <span>O agente pode tratar o primeiro caractere como comando.</span>}
        </div>
      )}
      {!open && pickError && <p role="alert" style={{ margin: 0, fontSize: 12, color: 'var(--err)' }}>{pickError}</p>}
      {error && <p role="alert" style={{ margin: 0, fontSize: 12, color: 'var(--err)' }}>{error}</p>}
      {!p.simple && <span style={{ fontSize: 12, color: 'var(--text-2)' }}>/ @ vão direto ao agente · # prompts · Ctrl K buscar</span>}
    </div>
  )
}

export const TileInput = memo(TileInputView)
