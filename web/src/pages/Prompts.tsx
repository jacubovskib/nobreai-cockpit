import { useEffect, useMemo, useState, type CSSProperties } from 'react'
import { Link } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError, del, patch, post } from '../api/client'
import { useCards, useProjects, usePromptEvents, usePrompts } from '../api/queries'
import type { Prompt } from '../api/types'
import { agentInfo } from '../ui/agents'
import { AgentSwatch } from '../ui/AgentSwatch'
import { ErrorLine, btn, btnPrimary, h2, input, muted, panel } from './card/shared'
import { useProjectFilter } from '../app/project'
import { KNOWN_VARS, splitVars, varName } from './prompts/highlight'

const ALL = 'Todos'
const AGENT_IDS = ['claude', 'codex', 'kiro']

interface Draft {
  name: string
  slug: string
  tag: string
  description: string
  body: string
  agents: string[]
  project_id: number | null
  is_default_handoff: boolean
}
const blank = (): Draft => ({ name: '', slug: '', tag: '', description: '', body: '', agents: [...AGENT_IDS], project_id: null, is_default_handoff: false })
const fromPrompt = (p: Prompt): Draft => ({
  name: p.name, slug: p.slug, tag: p.tag, description: p.description, body: p.body,
  agents: p.agents ?? [], project_id: p.project_id, is_default_handoff: p.is_default_handoff,
})

export function slugify(s: string) {
  const slug = s
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 40)
    .replace(/-+$/, '')
  return slug || 'prompt'
}

const chip = (on: boolean): CSSProperties => ({
  minHeight: 40, padding: '0 14px', borderRadius: 999, font: 'inherit', fontSize: 13, cursor: 'pointer',
  background: on ? '#2A2F3A' : 'transparent', color: on ? '#fff' : '#C3C8D2', border: `1px solid ${on ? '#4A505D' : '#2E333E'}`,
})
const field: CSSProperties = { display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13, color: 'var(--text-2)' }

function usedLabel(p: Prompt) {
  const n = `${p.uses} ${p.uses === 1 ? 'uso' : 'usos'}`
  return p.last_used_at ? `${n} · último ${new Date(p.last_used_at).toLocaleDateString('pt-BR', { day: 'numeric', month: 'short' })}` : n
}

function useDebounced<T>(v: T, ms = 300) {
  const [d, setD] = useState(v)
  useEffect(() => {
    const t = setTimeout(() => setD(v), ms)
    return () => clearTimeout(t)
  }, [v, ms])
  return d
}

function Preview({ body, cardKey }: { body: string; cardKey: string }) {
  const debounced = useDebounced(body)
  const q = useQuery({
    queryKey: ['prompt-preview', cardKey, debounced],
    enabled: !!cardKey && debounced !== '',
    retry: false,
    queryFn: () => post<{ text: string }>('/api/prompts/render', { card: cardKey, body: debounced }),
  })
  if (!cardKey) return <p style={muted}>Escolha um card para ver a prévia.</p>
  if (!debounced) return <p style={muted}>Escreva o texto para ver a prévia.</p>
  const err = q.error
  const unknown = err instanceof ApiError && err.status === 422 ? ((err.body as { unknown?: string[] } | null)?.unknown ?? []) : null
  if (unknown) {
    return (
      <div>
        <p role="alert" style={{ margin: '0 0 8px', fontSize: 13, color: 'var(--err)' }}>
          Variável desconhecida: {unknown.map((u) => `{{${u}}}`).join(', ')}
        </p>
        <div style={{ whiteSpace: 'pre-wrap' }}>
          {splitVars(debounced).map((p, i) =>
            p.isVar && unknown.includes(varName(p.text)) ? (
              <span key={i} style={{ color: 'var(--err)', textDecoration: 'underline wavy' }}>{p.text}</span>
            ) : (
              <span key={i}>{p.text}</span>
            ),
          )}
        </div>
      </div>
    )
  }
  if (err) return <ErrorLine error={err} />
  if (!q.data) return <p style={muted}>Renderizando…</p>
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      <div style={{ whiteSpace: 'pre-wrap' }}>{q.data.text}</div>
      {debounced.includes('{{card.pacote}}') && (
        <p style={{ ...muted, margin: 0, fontSize: 12 }}>
          Esta prévia é o texto do nobre prompt use, com o pacote. No Continuar, {'{{card.pacote}}'} fica vazio: o pacote chega no contexto da sessão nova (a prévia exata fica no painel Continuar do card).
        </p>
      )}
    </div>
  )
}

function Editor({ initial, isNew, onSaved, onDeleted, onDuplicate }: {
  initial: Draft
  isNew: boolean
  onSaved: (slug: string) => void
  onDeleted: () => void
  onDuplicate: (d: Draft) => void
}) {
  const qc = useQueryClient()
  const [d, setD] = useState<Draft>(initial)
  const [slugTouched, setSlugTouched] = useState(!isNew || initial.slug !== '')
  const [confirmDel, setConfirmDel] = useState(false)
  const cards = useCards().data ?? []
  const projects = useProjects().data ?? []
  const { project: chosenProject } = useProjectFilter()
  const [cardKey, setCardKey] = useState('')
  const previewCard = cardKey || cards[0]?.key || ''
  const set = (p: Partial<Draft>) => setD((x) => ({ ...x, ...p }))
  const save = useMutation({
    mutationFn: () => (isNew ? post<Prompt>('/api/prompts', d) : patch<Prompt>(`/api/prompts/${initial.slug}`, d)),
    onSuccess: async (p) => {
      await qc.invalidateQueries({ queryKey: ['prompts'] })
      onSaved(p.slug)
    },
  })
  const remove = useMutation({
    mutationFn: () => del(`/api/prompts/${initial.slug}`),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['prompts'] })
      onDeleted()
    },
  })
  const project = projects.find((p) => p.id === d.project_id)
  return (
    <section aria-labelledby="ed-t" style={panel}>
      <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap' }}>
        <h2 id="ed-t" style={h2}>{isNew ? 'Novo prompt' : 'Editar prompt'}</h2>
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: 12 }}>
        <label style={field}>Nome
          <input
            style={input}
            value={d.name}
            onChange={(e) => set({ name: e.target.value, ...(isNew && !slugTouched ? { slug: slugify(e.target.value) } : {}) })}
          />
        </label>
        <label style={field}>Atalho no CLI
          <input
            style={{ ...input, fontFamily: 'var(--mono)' }}
            value={d.slug}
            readOnly={!isNew}
            onChange={(e) => { setSlugTouched(true); set({ slug: e.target.value }) }}
          />
        </label>
        <label style={field}>Tag
          <input style={input} value={d.tag} onChange={(e) => set({ tag: e.target.value })} />
        </label>
      </div>
      <label style={field}>Descrição
        <input style={input} value={d.description} onChange={(e) => set({ description: e.target.value })} />
      </label>
      <label style={field}>Texto
        <textarea
          style={{ ...input, padding: 10, minHeight: 150, resize: 'vertical', fontFamily: 'var(--mono)', fontSize: 13 }}
          rows={7}
          value={d.body}
          onChange={(e) => set({ body: e.target.value })}
        />
      </label>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        <span style={muted}>Variáveis disponíveis (preenchidas a partir do card na hora de usar)</span>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
          {KNOWN_VARS.map((v) => (
            <button
              key={v}
              type="button"
              title={`Inserir ${v} no fim do texto`}
              onClick={() => set({ body: `${d.body}${d.body && !d.body.endsWith('\n') ? ' ' : ''}{{${v}}}` })}
              style={{ minHeight: 36, padding: '0 10px', borderRadius: 6, border: '1px solid #2E333E', background: '#0F1116', color: '#C9D4E6', fontFamily: 'var(--mono)', fontSize: 12, cursor: 'pointer' }}
            >
              {`{{${v}}}`}
            </button>
          ))}
        </div>
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6, padding: 12, border: '1px solid var(--border)', borderRadius: 8, background: 'var(--bg)' }}>
        <label style={{ ...muted, display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
          Prévia com o card
          <select style={{ ...input, minHeight: 36 }} value={previewCard} onChange={(e) => setCardKey(e.target.value)}>
            {cards.length === 0 && <option value="">nenhum card</option>}
            {cards.map((c) => <option key={c.key} value={c.key}>{c.key} · {c.title}</option>)}
          </select>
        </label>
        <Preview body={d.body} cardKey={previewCard} />
      </div>
      <fieldset style={{ border: 0, padding: 0, margin: 0, display: 'flex', flexWrap: 'wrap', gap: 16, alignItems: 'center' }}>
        <legend style={{ ...muted, padding: 0, marginBottom: 6 }}>Disponível para</legend>
        {AGENT_IDS.map((a) => (
          <label key={a} style={{ display: 'inline-flex', alignItems: 'center', gap: 8, minHeight: 40 }}>
            <input
              type="checkbox"
              checked={d.agents.includes(a)}
              onChange={(e) => set({ agents: e.target.checked ? [...d.agents, a] : d.agents.filter((x) => x !== a) })}
            />
            <span aria-hidden="true" style={{ width: 10, height: 10, borderRadius: 3, background: agentInfo(a).color }} />
            {agentInfo(a).name}
          </label>
        ))}
      </fieldset>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        <label style={{ display: 'flex', alignItems: 'center', gap: 8, minHeight: 36 }}>
          <input type="checkbox" checked={d.is_default_handoff} onChange={(e) => set({ is_default_handoff: e.target.checked })} />
          Usar como prompt inicial padrão ao continuar um card com outro agente
        </label>
        <label style={{ display: 'flex', alignItems: 'center', gap: 8, minHeight: 36, flexWrap: 'wrap' }}>
          <input
            type="checkbox"
            checked={d.project_id !== null}
            disabled={projects.length === 0}
            onChange={(e) => set({ project_id: e.target.checked ? (chosenProject?.id ?? projects[0]?.id ?? null) : null })}
          />
          Só neste projeto{project ? `: ${project.name}` : ''}
          {d.project_id !== null && projects.length > 1 && (
            <select aria-label="Projeto do prompt" style={{ ...input, minHeight: 36 }} value={d.project_id} onChange={(e) => set({ project_id: Number(e.target.value) })}>
              {projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
            </select>
          )}
        </label>
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        <span style={muted}>Pelo terminal</span>
        <code className="mono" style={{ padding: '10px 12px', borderRadius: 8, background: 'var(--bg)', border: '1px solid var(--border)', overflowX: 'auto' }}>
          nobre prompt use {d.slug || '<slug>'} --card {previewCard || '<KEY>'}
        </code>
      </div>
      <ErrorLine error={save.error ?? remove.error} />
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, justifyContent: 'flex-end' }}>
        {!isNew && !confirmDel && <button type="button" style={btn} onClick={() => setConfirmDel(true)}>Excluir</button>}
        {!isNew && confirmDel && (
          <>
            <button type="button" style={btn} onClick={() => setConfirmDel(false)}>Cancelar</button>
            <button type="button" style={{ ...btn, borderColor: 'var(--err)', color: 'var(--err)' }} disabled={remove.isPending} onClick={() => remove.mutate()}>
              Confirmar exclusão
            </button>
          </>
        )}
        {!isNew && <button type="button" style={btn} onClick={() => onDuplicate({ ...d, name: `${d.name} (cópia)`, slug: `${d.slug}-copia`, is_default_handoff: false })}>Duplicar</button>}
        {previewCard && <Link to={`/cards/${previewCard}`} style={btn}>Usar em um card…</Link>}
        <button type="button" style={btnPrimary} disabled={save.isPending} onClick={() => save.mutate()}>
          {save.isPending ? 'Salvando…' : 'Salvar'}
        </button>
      </div>
    </section>
  )
}

export function Prompts() {
  const prompts = usePrompts()
  const { projectId } = useProjectFilter()
  const events = usePromptEvents(50)
  const [q, setQ] = useState('')
  const [tag, setTag] = useState(ALL)
  const [sel, setSel] = useState<string | null>(null) // slug salvo, ou null
  const [draft, setDraft] = useState<{ n: number; d: Draft } | null>(null) // rascunho novo (n muda a key do editor)
  // Com projeto escolhido: prompts globais (project_id null) e os do projeto.
  const all = prompts.data ?? []
  const list = useMemo(() => all.filter((p) => projectId === null || p.project_id === null || p.project_id === projectId), [all, projectId])
  const tags = useMemo(() => [ALL, ...Array.from(new Set(list.map((p) => p.tag).filter(Boolean)))], [list])
  const term = q.trim().toLowerCase()
  const shown = list.filter(
    (p) => (tag === ALL || p.tag === tag) && (!term || `${p.name} ${p.slug} ${p.description}`.toLowerCase().includes(term)),
  )
  const current = draft ? null : (list.find((p) => p.slug === sel) ?? null)
  const newDraft = (d: Draft) => { setSel(null); setDraft({ n: (draft?.n ?? 0) + 1, d }) }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20, maxWidth: 1180 }}>
      <header style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'flex-end', justifyContent: 'space-between', gap: 16 }}>
        <div>
          <h1 style={{ fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em' }}>Prompts</h1>
          <p style={{ ...muted, margin: '4px 0 0' }}>Prompts salvos, com variáveis do card. Use pela interface, no handoff ou pelo terminal.</p>
        </div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
          <label htmlFor="busca-p" style={{ position: 'absolute', width: 1, height: 1, overflow: 'hidden', clip: 'rect(0 0 0 0)' }}>Buscar prompts</label>
          <input id="busca-p" type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Buscar prompt…" style={{ ...input, minHeight: 44, minWidth: 220 }} />
          <button type="button" style={btnPrimary} onClick={() => newDraft(blank())}>Novo prompt</button>
        </div>
      </header>
      {prompts.isError && <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>Não foi possível carregar os prompts.</p>}
      <div role="group" aria-label="Filtrar por tag" style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
        {tags.map((t) => (
          <button key={t} type="button" aria-pressed={tag === t} onClick={() => setTag(t)} style={chip(tag === t)}>{t}</button>
        ))}
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(340px, 1fr))', gap: 16, alignItems: 'start' }}>
        <section aria-label="Biblioteca" style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {prompts.isPending && !prompts.isError && <p className="placeholder">Carregando…</p>}
          {prompts.data && shown.length === 0 && <p className="placeholder">Nenhum prompt encontrado.</p>}
          {shown.map((p) => {
            const on = !draft && sel === p.slug
            return (
              <button
                key={p.slug}
                type="button"
                aria-pressed={on}
                onClick={() => { setDraft(null); setSel(p.slug) }}
                style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-start', gap: 6, width: '100%', padding: '14px 16px', borderRadius: 10, font: 'inherit', color: 'var(--text)', cursor: 'pointer', textAlign: 'left', background: on ? '#1D212A' : 'var(--surface)', border: `1px solid ${on ? '#6B7280' : 'var(--border)'}` }}
              >
                <span style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
                  <strong>{p.name}</strong>
                  {p.is_default_handoff && <span className="badge">padrão no handoff</span>}
                </span>
                {p.description && <span style={muted}>{p.description}</span>}
                <span style={{ ...muted, display: 'flex', gap: 12, flexWrap: 'wrap', fontSize: 12 }}>
                  <span className="mono">{p.slug}</span>
                  {p.tag && <span>{p.tag}</span>}
                  <span>{usedLabel(p)}</span>
                </span>
              </button>
            )
          })}
        </section>
        {draft ? (
          <Editor
            key={`new-${draft.n}`}
            initial={draft.d}
            isNew
            onSaved={(slug) => { setDraft(null); setSel(slug) }}
            onDeleted={() => setDraft(null)}
            onDuplicate={newDraft}
          />
        ) : current ? (
          <Editor
            key={current.slug}
            initial={fromPrompt(current)}
            isNew={false}
            onSaved={() => undefined}
            onDeleted={() => setSel(null)}
            onDuplicate={newDraft}
          />
        ) : (
          <section style={panel}><p style={muted}>Escolha um prompt na biblioteca ou crie um novo.</p></section>
        )}
      </div>

      <section aria-labelledby="cap-t" style={panel}>
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'baseline', gap: 12 }}>
          <h2 id="cap-t" style={h2}>Prompts que você digitou nas sessões</h2>
          <span style={muted}>Capturados pelos hooks · salve os que quiser repetir</span>
        </div>
        {events.isError && <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>Não foi possível carregar os prompts capturados.</p>}
        {events.data && events.data.length === 0 && <p style={muted}>Nenhum prompt capturado ainda.</p>}
        <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column' }}>
          {(events.data ?? []).map((e) => {
            const text = (e.data as { text?: string } | null)?.text ?? ''
            if (!text) return null
            const saved = list.some((p) => p.body === text)
            return (
              <li key={e.id} style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 12, padding: '12px 0', borderTop: '1px solid var(--border)' }}>
                <span style={{ ...muted, display: 'inline-flex', gap: 6, alignItems: 'center', minWidth: 240 }}>
                  {new Date(e.at).toLocaleString('pt-BR', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })} ·{' '}
                  {e.agent ? <AgentSwatch agent={e.agent} /> : 'agente'} ·{' '}
                  {e.card_key ? <Link to={`/cards/${e.card_key}`}>{e.card_key}</Link> : 'sem card'}
                </span>
                <span style={{ flex: 1, minWidth: 200, whiteSpace: 'pre-wrap' }}>“{text}”</span>
                {saved ? (
                  <span style={{ ...muted, color: 'var(--ok)' }}>✓ Salvo na biblioteca</span>
                ) : (
                  <button
                    type="button"
                    style={btn}
                    aria-label={`Salvar como prompt: ${text.slice(0, 40)}`}
                    onClick={() => newDraft({ ...blank(), body: text, name: text.slice(0, 40), slug: slugify(text) })}
                  >
                    Salvar como prompt
                  </button>
                )}
              </li>
            )
          })}
        </ul>
      </section>
    </div>
  )
}
