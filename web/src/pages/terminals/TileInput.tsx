import { memo, useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { Project } from '../../api/types'
import { inputError, insertAt, sendInput } from './inputModel'
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

function TileInputView(p: TileInputProps) {
  const [freed, setFreed] = useState(false)
  const [text, setText] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const area = useRef<HTMLTextAreaElement>(null)
  const textRef = useRef(text)
  textRef.current = text
  const caret = useRef<number | null>(null) // cursor a aplicar depois do próximo render
  const busy = useRef(false)
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
      pickPrompt: focus, // a lista do # completa isto
      focus,
    })
  }, [p.terminalId, bar])

  useLayoutEffect(() => {
    if (caret.current === null) return
    area.current?.setSelectionRange(caret.current, caret.current)
    caret.current = null
  }, [text])

  const send = async () => {
    const t = textRef.current
    if (busy.current || t.trim() === '') return
    busy.current = true
    setPending(true)
    try {
      await sendInput(p.terminalId, t)
      setText('')
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

  const rows = Math.min(MAX_ROWS, Math.max(1, text.split('\n').length))
  return (
    <div data-testid="tile-input" style={footer}>
      <div style={{ display: 'flex', gap: 6, alignItems: 'flex-end' }}>
        <textarea
          ref={area}
          aria-label={'Enviar ao agente ' + p.agentName}
          placeholder="Enviar ao agente…  # prompts salvos"
          rows={rows}
          value={text}
          onChange={(e) => { setText(e.target.value); setError('') }}
          onKeyDown={(e) => {
            if (e.key !== 'Enter' || e.shiftKey || e.nativeEvent.isComposing) return
            e.preventDefault()
            void send()
          }}
          style={{ flex: 1, minWidth: 0, resize: 'none', padding: '4px 8px', borderRadius: 6, border: '1px solid #3A3F4B', background: 'var(--bg)', color: 'var(--text)', font: 'inherit' }}
        />
        <button type="button" style={btn} disabled={pending || text.trim() === ''} onClick={() => void send()}>Enviar</button>
      </div>
      {error && <p role="alert" style={{ margin: 0, fontSize: 12, color: 'var(--err)' }}>{error}</p>}
      {!p.simple && <span style={{ fontSize: 12, color: 'var(--text-2)' }}>/ @ vão direto ao agente · # prompts · Ctrl K buscar</span>}
    </div>
  )
}

export const TileInput = memo(TileInputView)
