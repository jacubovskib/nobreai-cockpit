import { useEffect, useMemo, useRef, useState } from 'react'
import { useCards, useMemories, usePrompts } from '../../api/queries'
import type { Card, Memory, Project, Prompt } from '../../api/types'
import { MAX_GROUP, cardPiece, memoryPiece, paletteCards, paletteMemories, promptsFor } from './inputModel'
import { inputFor } from './inputRegistry'

export interface PaletteProps {
  targetId: string | null // tile em foco na página
  targetLabel: string // ex.: "Claude Code · NAC-63"; '' sem alvo
  agent: string // '' sem alvo
  project: Project | null // projeto do alvo
  onClose(): void
}

const NO_PROMPTS: Prompt[] = []
const NO_CARDS: Card[] = []
const NO_MEMS: Memory[] = []

interface Item { key: string; label: string; hint: string; pick(h: NonNullable<ReturnType<typeof inputFor>>): void }
interface Group { title: string; items: Item[] }

const overlay: React.CSSProperties = {
  position: 'fixed', inset: 0, zIndex: 50, background: 'rgba(0,0,0,.5)', display: 'flex', justifyContent: 'center', alignItems: 'flex-start', paddingTop: '12vh',
}
const box: React.CSSProperties = {
  width: 'min(560px, calc(100vw - 32px))', maxHeight: '70vh', overflowY: 'auto', background: 'var(--surface, #1B1F27)',
  border: '1px solid #3A404D', borderRadius: 12, padding: 12, display: 'flex', flexDirection: 'column', gap: 10,
}
const muted: React.CSSProperties = { margin: 0, color: 'var(--text-2)', fontSize: 13 }

export function Palette({ targetId, targetLabel, agent, project, onClose }: PaletteProps) {
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const promptsQ = usePrompts()
  const cardsQ = useCards()
  const memsQ = useMemories(project?.id ?? null, true)
  const prompts = Array.isArray(promptsQ.data) ? promptsQ.data : NO_PROMPTS
  const cards = Array.isArray(cardsQ.data) ? cardsQ.data : NO_CARDS
  const mems = Array.isArray(memsQ.data) ? memsQ.data : NO_MEMS
  const usable = inputFor(targetId) !== null
  const q = query.trim()

  useEffect(() => { inputRef.current?.focus() }, [])

  const groups = useMemo<Group[]>(() => {
    // Sem alvo não há agente nem projeto para filtrar: vale só a busca.
    const found = targetId === null
      ? promptsFor(prompts.map((p) => ({ ...p, agents: [''], project_id: null })), '', null, q)
      : promptsFor(prompts, agent, project, q)
    const out: Group[] = [
      {
        title: 'Prompts salvos',
        items: found.slice(0, MAX_GROUP).map((p) => ({ key: 'p' + p.id, label: p.name, hint: '#' + p.slug, pick: (h) => h.pickPrompt(p.slug) })),
      },
      {
        title: 'Cards',
        items: paletteCards(cards, q).map((c) => ({ key: 'c' + c.id, label: `${c.key} · ${c.title}`, hint: '', pick: (h) => h.insert(cardPiece(c)) })),
      },
    ]
    if (q !== '') {
      out.push({
        title: 'Memórias',
        items: paletteMemories(mems, project, q).map((m) => ({ key: 'm' + m.id, label: memoryPiece(m), hint: '', pick: (h) => h.insert(memoryPiece(m)) })),
      })
    }
    return out.filter((g) => g.items.length > 0)
  }, [prompts, cards, mems, targetId, agent, project, q])

  const flat = useMemo(() => groups.flatMap((g) => g.items), [groups])
  const mark = Math.min(active, Math.max(flat.length - 1, 0))

  const choose = (it: Item) => {
    const h = inputFor(targetId)
    if (!h) return
    onClose()
    it.pick(h)
  }

  // Teclas na janela, não no diálogo: clicar num título, no espaço vazio ou
  // num item desligado manda o foco para o body, e Esc/setas/Enter seguem
  // valendo. O ref evita reassinar a cada render.
  const onKey = useRef<(e: KeyboardEvent) => void>(() => {})
  onKey.current = (e) => {
    if (e.isComposing) return
    if (e.key === 'Escape') { e.preventDefault(); onClose(); return }
    if (!usable || flat.length === 0) return
    if (e.key === 'ArrowDown') { e.preventDefault(); setActive((mark + 1) % flat.length) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setActive((mark - 1 + flat.length) % flat.length) }
    else if (e.key === 'Enter') { e.preventDefault(); choose(flat[mark]) }
  }
  useEffect(() => {
    const h = (e: KeyboardEvent) => onKey.current(e)
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [])

  return (
    <div style={overlay} onMouseDown={(e) => { if (e.target === e.currentTarget) onClose() }}>
      <div role="dialog" aria-modal="true" aria-label="Buscar" style={box}>
        <input
          ref={inputRef}
          aria-label="Buscar"
          placeholder="Prompt salvo, card ou memória"
          value={query}
          onChange={(e) => { setQuery(e.target.value); setActive(0) }}
          style={{ font: 'inherit', padding: '8px 10px', borderRadius: 8, border: '1px solid #3A404D', background: 'transparent', color: 'inherit' }}
        />
        <p style={muted}>{usable ? `Inserir em: ${targetLabel}` : 'Clique num terminal para escolher onde inserir.'}</p>
        {flat.length === 0 && <p style={muted}>Nada encontrado.</p>}
        {groups.map((g) => (
          <div key={g.title} role="group" aria-label={g.title} style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            <h2 style={{ margin: '4px 0', fontSize: 12, fontWeight: 600, color: 'var(--text-2)' }}>{g.title}</h2>
            {g.items.map((it) => {
              const on = usable && flat[mark] === it
              return (
                <button
                  key={it.key}
                  type="button"
                  disabled={!usable}
                  aria-current={on ? 'true' : undefined}
                  onClick={() => choose(it)}
                  onMouseMove={() => { if (usable) setActive(flat.indexOf(it)) }}
                  style={{
                    font: 'inherit', textAlign: 'left', padding: '6px 10px', borderRadius: 8, border: 0, cursor: usable ? 'pointer' : 'default',
                    background: on ? '#2A2F3A' : 'transparent', color: usable ? 'inherit' : 'var(--text-2)',
                    display: 'flex', gap: 8, justifyContent: 'space-between', overflow: 'hidden',
                  }}
                >
                  <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{it.label}</span>
                  {it.hint && <span style={{ color: 'var(--text-2)' }}>{it.hint}</span>}
                </button>
              )
            })}
          </div>
        ))}
      </div>
    </div>
  )
}
