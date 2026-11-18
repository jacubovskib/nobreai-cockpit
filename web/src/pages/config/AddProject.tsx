import { useState, type CSSProperties } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { get, post } from '../../api/client'
import type { BatchResult, FsScan, ScanSuggestion } from '../../api/types'
import { ErrorLine, btn, btnPrimary, input, muted } from '../card/shared'
import { FolderPicker } from './FolderPicker'

const KIND_LABEL = { repo: 'Repositório', monorepo: 'Monorepo (apps sugeridos)', group: 'Pasta com repositórios (grupo)' }
const field: CSSProperties = { display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13, color: 'var(--text-2)' }
const row: CSSProperties = { display: 'flex', alignItems: 'center', gap: 10, minHeight: 32 }

interface Item { path: string; rel: string; name: string; registered: boolean; blocked: string; manual: boolean }

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`
const baseName = (p: string) => p.replace(/\/+$/, '').split('/').pop() ?? p

// Texto do resultado do cadastro (singular e plural).
export function registeredText(name: string, kids: number, moved: { sessions: number; cards: number }): string[] {
  const out = [`${name} cadastrado com ${plural(kids, 'sub-projeto', 'sub-projetos')}.`]
  if (moved.sessions + moved.cards > 0) out.push(`${plural(moved.sessions, 'sessão', 'sessões')} e ${plural(moved.cards, 'card', 'cards')} passaram para os sub-projetos.`)
  return out
}

function toItem(s: ScanSuggestion): Item {
  return { path: s.path, rel: s.rel, name: s.name, registered: s.registered != null, blocked: s.blocked, manual: false }
}

function Result({ path, onClose }: { path: string; onClose: () => void }) {
  const qc = useQueryClient()
  const scan = useQuery({ queryKey: ['fs-scan', path], queryFn: () => get<FsScan>(`/api/fs/scan?path=${encodeURIComponent(path)}`), retry: false, refetchOnWindowFocus: false })
  const [name, setName] = useState<string | null>(null)
  const [off, setOff] = useState<string[]>([])
  const [manual, setManual] = useState<Item[]>([])
  const [picking, setPicking] = useState(false)
  const [dismissOff, setDismissOff] = useState(false)
  const reg = useMutation({
    mutationFn: async (v: { body: object; rels: string[] }) => {
      const r = await post<BatchResult>('/api/projects/batch', v.body)
      // O cadastro já foi gravado; recusar as pastas desmarcadas vem depois.
      if (v.rels.length > 0) await post(`/api/projects/${r.project.id}/suggestions/dismiss`, { rels: v.rels })
      return r
    },
    onSettled: () => Promise.all(['projects', 'cards', 'suggestions'].map((k) => qc.invalidateQueries({ queryKey: [k] }))),
  })
  const data = scan.data
  const self = data?.registered ?? null
  if (reg.data) {
    return (
      <div role="status" style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        {registeredText(reg.data.project.name, reg.data.children.length, reg.data.moved).map((t) => <span key={t} style={{ color: 'var(--ok)' }}>{t}</span>)}
        <div><button type="button" style={btn} onClick={onClose}>Fechar</button></div>
      </div>
    )
  }
  if (picking) {
    return <FolderPicker start={path} onCancel={() => setPicking(false)} onUse={(p) => {
      const rel = p.startsWith(path + '/') ? p.slice(path.length + 1) : p
      setManual((m) => (m.some((x) => x.path === p) || data?.suggestions.some((s) => s.path === p) ? m : [...m, { path: p, rel, name: baseName(p), registered: false, blocked: '', manual: true }]))
      setPicking(false)
    }} />
  }
  if (scan.isPending) return <p style={muted}>Varrendo a pasta…</p>
  if (!data) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        <ErrorLine error={scan.error} />
        <div><button type="button" style={btn} onClick={onClose}>Fechar</button></div>
      </div>
    )
  }
  // Numa nova varredura, quem já é filho deste projeto não é novidade.
  const items = [...data.suggestions.filter((s) => !(self && s.registered?.parent_id === self.id)).map(toItem), ...manual]
  const chosen = items.filter((i) => !off.includes(i.rel) && i.blocked === '')
  const unchecked = items.filter((i) => off.includes(i.rel) && i.blocked === '' && !i.manual).map((i) => i.rel)
  const shownName = self ? self.name : name ?? data.name
  const submit = () => reg.mutate({
    rels: dismissOff ? unchecked : [],
    body: {
      path: data.path,
      ...(!self && shownName.trim() !== '' ? { name: shownName.trim() } : {}),
      children: chosen.map((i) => (i.registered ? { path: i.path } : { path: i.path, name: i.name })),
    },
  })
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'flex-end', gap: 12 }}>
        <span><strong>{data.name}</strong> · {KIND_LABEL[data.kind]}{self && <span className="badge" style={{ marginLeft: 8 }}>cadastrado</span>}</span>
        <label style={field}>Nome
          <input style={input} value={shownName} disabled={self != null} onChange={(e) => setName(e.target.value)} />
        </label>
      </div>
      {items.length === 0 && <p style={muted}>Nenhum sub-projeto novo.</p>}
      {items.map((i) => (
        <label key={i.rel} style={row}>
          <input type="checkbox" checked={i.blocked === '' && !off.includes(i.rel)} disabled={i.blocked !== ''}
            onChange={(e) => setOff((o) => (e.target.checked ? o.filter((r) => r !== i.rel) : [...o, i.rel]))} />
          <span>
            {i.name} <span className="mono" style={{ ...muted, fontSize: 12 }}>{i.rel}</span>
            {i.blocked !== '' && <span style={muted}> ({i.blocked})</span>}
            {i.blocked === '' && i.registered && <span style={muted}> (já cadastrado: entra no grupo)</span>}
          </span>
        </label>
      ))}
      <div><button type="button" style={btn} onClick={() => setPicking(true)}>Adicionar outra pasta como sub-projeto…</button></div>
      <label style={row}>
        <input type="checkbox" checked={dismissOff} onChange={(e) => setDismissOff(e.target.checked)} />
        não sugerir as pastas desmarcadas
      </label>
      <ErrorLine error={reg.error} />
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        <button type="button" style={btnPrimary} disabled={reg.isPending} onClick={submit}>Cadastrar</button>
        <button type="button" style={btn} onClick={onClose}>Cancelar</button>
      </div>
    </div>
  )
}

// Adicionar projeto: escolhe a pasta, varre e cadastra tudo de uma vez. Com
// initialPath (Varrer de novo) abre direto no resultado.
export function AddProject({ initialPath, onClose }: { initialPath?: string; onClose: () => void }) {
  const [path, setPath] = useState<string | null>(initialPath ?? null)
  return (
    <section aria-label="Adicionar projeto" style={{ display: 'flex', flexDirection: 'column', gap: 10, padding: 14, borderRadius: 8, border: '1px solid var(--border)' }}>
      <strong>Adicionar projeto</strong>
      {path === null ? <FolderPicker onUse={setPath} onCancel={onClose} /> : <Result path={path} onClose={onClose} />}
    </section>
  )
}
