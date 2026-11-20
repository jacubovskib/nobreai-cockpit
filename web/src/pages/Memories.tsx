import { useEffect, useMemo, useState, type CSSProperties } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { del, patch, post } from '../api/client'
import { useMemories, useMemoryPreview, useProjects, useSettings } from '../api/queries'
import type { Memory, MemoryBatch, MemoryImport } from '../api/types'
import { useProjectFilter } from '../app/project'
import { ErrorLine, btn, btnPrimary, h2, input, muted, panel } from './card/shared'
import { CATEGORIES, DO_MAX, RULE_MAX, len, similarTo } from './memories/text'

const field: CSSProperties = { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13, color: 'var(--text-2)' }
const list: CSSProperties = { listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 8 }
const item: CSSProperties = { display: 'flex', flexDirection: 'column', gap: 6, padding: 12, borderRadius: 8, border: '1px solid var(--border)' }
const tag: CSSProperties = { fontSize: 12, padding: '2px 8px', borderRadius: 999, border: '1px solid #3A3F4B', color: 'var(--text-2)' }
const bar: CSSProperties = { display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'center' }

const catLabel = (id: string) => CATEGORIES.find((c) => c.id === id)?.label ?? id

// sourceLabel: na fila o napkin traz o caminho; nas ativas, só "napkin" (o caminho fica no title).
function sourceLabel(m: Memory, short = false) {
  if (m.source === 'napkin') return short ? 'napkin' : `napkin: ${m.source_ref}`
  if (m.source === 'card') return `card ${m.source_ref}`
  return m.source === 'agente' ? 'agente' : 'você'
}

// approveHint: por que a candidata ainda não pode ser aprovada ("" = pode).
function approveHint(m: Memory) {
  if (len(m.rule) > RULE_MAX) return `edite: regra acima de ${RULE_MAX}`
  if (len(m.do) > DO_MAX) return `edite: ação acima de ${DO_MAX}`
  if (m.do.trim() === '') return 'edite: falta o faça assim'
  return ''
}

function Counter({ n, max }: { n: number; max: number }) {
  return <span style={{ color: n > max ? 'var(--err)' : 'var(--text-2)', fontSize: 12 }}>{n}/{max}</span>
}

interface Scope { value: string; label: string }

interface Draft { rule: string; do: string; category: string; scope: string }

// requireDo: memória ativa precisa do "faça assim"; candidata pode salvar sem.
function Editor({ draft, scopes, submit, cancel, busy, submitLabel, requireDo }: {
  draft: Draft
  scopes: Scope[]
  submit(d: Draft): void
  cancel?: () => void
  busy: boolean
  submitLabel: string
  requireDo: boolean
}) {
  const [d, setD] = useState(draft)
  const bad = d.rule.trim() === '' || len(d.rule) > RULE_MAX || len(d.do) > DO_MAX || (requireDo && d.do.trim() === '')
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      <div style={field}>
        <span>Regra <Counter n={len(d.rule)} max={RULE_MAX} /></span>
        <textarea aria-label="Regra" rows={2} style={{ ...input, padding: 10 }} value={d.rule} onChange={(e) => setD({ ...d, rule: e.target.value })} />
      </div>
      <div style={field}>
        <span>Faça assim <Counter n={len(d.do)} max={DO_MAX} /></span>
        <textarea aria-label="Faça assim" rows={2} style={{ ...input, padding: 10 }} value={d.do} onChange={(e) => setD({ ...d, do: e.target.value })} />
      </div>
      <div style={bar}>
        <label style={field}>
          Categoria
          <select style={input} value={d.category} onChange={(e) => setD({ ...d, category: e.target.value })}>
            {CATEGORIES.map((c) => <option key={c.id} value={c.id}>{c.label}</option>)}
          </select>
        </label>
        <label style={field}>
          Escopo
          <select style={input} value={d.scope} onChange={(e) => setD({ ...d, scope: e.target.value })}>
            {scopes.map((s) => <option key={s.value} value={s.value}>{s.label}</option>)}
          </select>
        </label>
        <button type="button" style={{ ...btnPrimary, alignSelf: 'flex-end' }} disabled={bad || busy} onClick={() => submit(d)}>{submitLabel}</button>
        {cancel && <button type="button" style={{ ...btn, alignSelf: 'flex-end' }} onClick={cancel}>Cancelar</button>}
      </div>
    </div>
  )
}

export function Memories() {
  const qc = useQueryClient()
  const { projectId, root, app } = useProjectFilter()
  const projectsQ = useProjects()
  const projects = projectsQ.data ?? []
  // Raiz sem app escolhido: a lista inclui as memórias dos apps, marcadas com o nome.
  const family = root !== null && app === null
  const memories = useMemories(projectId, family)
  const preview = useMemoryPreview(projectId)
  const budget = useSettings().data?.memory_budget_tokens
  const [formKey, setFormKey] = useState(0)
  const [selected, setSelected] = useState<number[]>([])
  const [editing, setEditing] = useState<number | null>(null)
  const [confirming, setConfirming] = useState<number | null>(null)
  const [refused, setRefused] = useState<MemoryBatch['refused']>([])
  useEffect(() => {
    setSelected([])
    setRefused([])
    setEditing(null)
    setConfirming(null)
  }, [projectId])
  const [imported, setImported] = useState<MemoryImport | null>(null)

  const act = useMutation<unknown, Error, () => Promise<unknown>>({
    mutationFn: (f) => f(),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['memories'] }),
  })
  const run = (f: () => Promise<unknown>, after?: () => void) => act.mutate(f, { onSuccess: after })

  const all = memories.data ?? []
  const byStatus = (s: Memory['status']) => all.filter((m) => m.status === s)
  const queue = byStatus('candidata')
  const rank = (m: Memory) => (m.project_id === null ? 2 : m.project_id === projectId || projectId === null ? 0 : 1)
  const actives = byStatus('ativa').sort((a, b) => rank(a) - rank(b) || a.position - b.position || a.id - b.id)
  const archived = byStatus('arquivada')
  const visible = useMemo(() => all.filter((m) => m.status === 'ativa' || m.status === 'candidata'), [all])

  const scopesFor = (m: Memory | null): Scope[] => {
    const out: Scope[] = []
    const add = (value: string, label: string) => { if (!out.some((s) => s.value === value)) out.push({ value, label }) }
    // "este projeto" é o projeto/app da página; em Todos, o do próprio item.
    const here = projectId ?? m?.project_id ?? null
    if (here !== null) add(String(here), 'este projeto')
    const parent = projects.find((p) => p.id === here)?.parent_id ?? null
    if (parent !== null) add(String(parent), 'raiz')
    add('g', 'global')
    if (m && m.project_id !== null) add(String(m.project_id), projects.find((p) => p.id === m.project_id)?.name ?? 'outro projeto')
    return out
  }
  const scopeValue = (m: Memory | null) => (m ? (m.project_id === null ? 'g' : String(m.project_id)) : projectId === null ? 'g' : String(projectId))
  const pid = (scope: string) => (scope === 'g' ? null : Number(scope))

  const note = (m: Memory) => {
    const s = similarTo(m, visible)
    return s ? <span style={tag}>{s.kind === 'duplicada' ? `Duplicada de #${s.id}` : `Parecida com #${s.id}`}</span> : null
  }
  const origin = (m: Memory) => {
    if (m.inherited) return <span style={tag}>{m.project_id === null ? 'global' : 'da raiz'}</span>
    // Item de um app na lista da raiz.
    if (projectId !== null && m.project_id !== null && m.project_id !== projectId) {
      return <span style={tag}>app {projects.find((p) => p.id === m.project_id)?.name ?? `#${m.project_id}`}</span>
    }
    return null
  }
  const idTag = (m: Memory) => <span style={muted}>#{m.id}</span>

  const body = (m: Memory) => (
    <>
      <div><strong>{m.rule}</strong>{len(m.rule) > RULE_MAX && <> <Counter n={len(m.rule)} max={RULE_MAX} /></>}</div>
      <div style={muted}>→ {m.do}{len(m.do) > DO_MAX && <> <Counter n={len(m.do)} max={DO_MAX} /></>}</div>
    </>
  )

  const editor = (m: Memory) => (
    <Editor
      draft={{ rule: m.rule, do: m.do, category: m.category, scope: scopeValue(m) }}
      scopes={scopesFor(m)}
      busy={act.isPending}
      submitLabel="Salvar"
      requireDo={m.status === 'ativa'}
      cancel={() => setEditing(null)}
      submit={(d) => run(() => patch(`/api/memories/${m.id}`, { rule: d.rule, do: d.do, category: d.category, project_id: pid(d.scope) }), () => setEditing(null))}
    />
  )

  const setStatus = (m: Memory, status: string, extra: object = {}) =>
    run(() => patch(`/api/memories/${m.id}`, { status, ...extra }))

  const batch = () =>
    run(async () => {
      const r = await post<MemoryBatch>('/api/memories/batch', { ids: selected, action: 'aprovar' })
      setRefused(r.refused)
      setSelected(selected.filter((id) => r.refused.some((x) => x.id === id)))
    })

  if (projectsQ.isPending || !memories.data) return <p style={muted}>Carregando…</p>
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div style={{ ...bar, justifyContent: 'space-between' }}>
        <h1 style={{ margin: 0, fontSize: 20 }}>Memórias</h1>
        <button type="button" style={btn} disabled={act.isPending} onClick={() => run(async () => setImported(await post<MemoryImport>('/api/memories/import')))}>Importar napkin.md</button>
      </div>
      <ErrorLine error={act.error ?? memories.error} />
      {imported && (
        <div style={panel} role="status">
          <span>{imported.created} candidatas novas, {imported.skipped} já existentes, {imported.merged ?? 0} unidas em globais</span>
          <ul style={{ ...list, ...muted }}>
            {imported.files.map((f) => <li key={f.path} style={f.error ? { color: 'var(--err)' } : undefined}>{f.error ? `${f.path}: erro ${f.error}` : `${f.path} (${f.items})`}</li>)}
          </ul>
        </div>
      )}

      <section style={panel} aria-label="Fila de candidatas">
        <h2 style={h2}>Fila de candidatas ({queue.length})</h2>
        {queue.length === 0 ? <p style={muted}>Nenhuma candidata para revisar.</p> : (
          <>
            <div style={bar}>
              <button type="button" style={btnPrimary} disabled={selected.length === 0 || act.isPending} onClick={batch}>Aprovar selecionadas</button>
            </div>
            {refused.length > 0 && (
              <ul style={{ ...list, color: 'var(--err)', fontSize: 13 }} role="alert">
                {refused.map((r) => <li key={r.id}>#{r.id}: {r.error}</li>)}
              </ul>
            )}
            <ul style={list}>
              {queue.map((m) => (
                <li key={m.id} style={item}>
                  {editing === m.id ? editor(m) : (
                    <>
                      <div style={bar}>
                        <input type="checkbox" aria-label={`Selecionar #${m.id}`} checked={selected.includes(m.id)}
                          onChange={(e) => setSelected(e.target.checked ? [...selected, m.id] : selected.filter((x) => x !== m.id))} />
                        {idTag(m)}
                        <span style={tag}>{catLabel(m.category)}</span>
                        {origin(m)}
                        <span style={muted}>{sourceLabel(m)}</span>
                        {m.needs_review && <span style={{ ...tag, color: 'var(--err)' }}>revisar</span>}
                        {note(m)}
                      </div>
                      {body(m)}
                      <div style={bar}>
                        <button type="button" style={btnPrimary} disabled={approveHint(m) !== '' || act.isPending} onClick={() => setStatus(m, 'ativa', { needs_review: false })}>Aprovar</button>
                        {approveHint(m) && <span style={{ fontSize: 12, color: 'var(--err)' }}>{approveHint(m)}</span>}
                        <button type="button" style={btn} onClick={() => setEditing(m.id)}>Editar</button>
                        <button type="button" style={btn} disabled={act.isPending} onClick={() => setStatus(m, 'descartada')}>Descartar</button>
                      </div>
                    </>
                  )}
                </li>
              ))}
            </ul>
          </>
        )}
      </section>

      <section style={panel} aria-label="Memórias ativas">
        <h2 style={h2}>Memórias ativas ({actives.length})</h2>
        {actives.length === 0 && <p style={muted}>Nenhuma memória ativa.</p>}
        {CATEGORIES.map((c) => {
          const rows = actives.filter((m) => m.category === c.id)
          if (rows.length === 0) return null
          return (
            <div key={c.id} style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              <h3 style={{ margin: 0, fontSize: 13, color: 'var(--text-2)' }}>{c.label}</h3>
              <ul style={list}>
                {rows.map((m) => (
                  <li key={m.id} style={item}>
                    {editing === m.id ? editor(m) : (
                      <>
                        <div style={bar}>
                          {idTag(m)}{origin(m)}
                          <span style={muted} title={m.source === 'napkin' ? m.source_ref : undefined}>{sourceLabel(m, true)}</span>
                          {note(m)}{m.needs_review && <span style={{ ...tag, color: 'var(--err)' }}>revisar</span>}
                        </div>
                        {body(m)}
                        <div style={bar}>
                          {!m.inherited && (
                            <>
                              <button type="button" style={btn} aria-label="Subir" disabled={act.isPending} onClick={() => run(() => post(`/api/memories/${m.id}/move`, { dir: -1 }))}>↑</button>
                              <button type="button" style={btn} aria-label="Descer" disabled={act.isPending} onClick={() => run(() => post(`/api/memories/${m.id}/move`, { dir: 1 }))}>↓</button>
                            </>
                          )}
                          <button type="button" style={btn} onClick={() => setEditing(m.id)}>Editar</button>
                          <button type="button" style={btn} disabled={act.isPending} onClick={() => setStatus(m, 'arquivada')}>Arquivar</button>
                        </div>
                      </>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          )
        })}
      </section>

      <section style={panel} aria-label="Prévia da sessão">
        <h2 style={h2}>Prévia da sessão</h2>
        {projectId === null ? <p style={muted}>Escolha um projeto para ver a prévia.</p> : budget === 0 ? <p style={muted}>Memórias desligadas (limite 0 na Configuração)</p> : preview.error ? <ErrorLine error={preview.error} /> : !preview.data ? <p style={muted}>Carregando…</p> : (
          <>
            <p style={{ margin: 0 }}>Entram {preview.data.chosen} de {preview.data.chosen + preview.data.left} memórias (~{preview.data.tokens} tokens) na próxima sessão deste projeto</p>
            {preview.data.block && (
              <details>
                <summary style={{ cursor: 'pointer', ...muted }}>Ver o bloco</summary>
                <pre style={{ whiteSpace: 'pre-wrap', fontSize: 12, margin: '8px 0 0' }}>{preview.data.block}</pre>
              </details>
            )}
          </>
        )}
      </section>

      <section style={panel} aria-label="Nova memória">
        <h2 style={h2}>Nova memória</h2>
        <Editor
          key={`${projectId}-${formKey}`}
          draft={{ rule: '', do: '', category: 'dominio', scope: scopeValue(null) }}
          scopes={scopesFor(null)}
          busy={act.isPending}
          submitLabel="Criar"
          requireDo
          submit={(d) => run(() => post('/api/memories', { project_id: pid(d.scope), category: d.category, rule: d.rule, do: d.do }), () => setFormKey((k) => k + 1))}
        />
      </section>

      <section style={panel} aria-label="Arquivadas">
        <h2 style={h2}>Arquivadas ({archived.length})</h2>
        {archived.length === 0 && <p style={muted}>Nenhuma memória arquivada.</p>}
        <ul style={list}>
          {archived.map((m) => (
            <li key={m.id} style={item}>
              <div style={bar}>{idTag(m)}{origin(m)}</div>
              {body(m)}
              <div style={bar}>
                {confirming === m.id ? (
                  <>
                    <span style={muted}>Excluir #{m.id}? Ela some da página e não volta na importação.</span>
                    <button type="button" style={btnPrimary} disabled={act.isPending} onClick={() => run(() => del(`/api/memories/${m.id}`), () => setConfirming(null))}>Confirmar</button>
                    <button type="button" style={btn} onClick={() => setConfirming(null)}>Cancelar</button>
                  </>
                ) : (
                  <>
                    <button type="button" style={btn} disabled={act.isPending || m.do.trim() === ''} onClick={() => setStatus(m, 'ativa')}>Restaurar</button>
                    <button type="button" style={btn} onClick={() => setConfirming(m.id)}>Excluir</button>
                  </>
                )}
              </div>
            </li>
          ))}
        </ul>
      </section>
    </div>
  )
}
