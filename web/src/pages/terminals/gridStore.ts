// Persistência do layout da grade no navegador (localStorage), tolerante a dados velhos ou corrompidos.
import { DEF_H, clampH, defaultLayout, type Cols, type Layout, type TileSize } from './gridModel'

export const KEY = 'nobre.terminais.grade.v1'

const isObj = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)

function parseSizes(v: unknown): Record<string, TileSize> {
  const out: Record<string, TileSize> = {}
  if (!isObj(v)) return out
  for (const [id, s] of Object.entries(v)) {
    if (!isObj(s)) continue
    const span = typeof s.span === 'number' && Number.isInteger(s.span) && s.span > 0 ? s.span : 1
    const h = typeof s.h === 'number' && Number.isFinite(s.h) ? clampH(s.h) : DEF_H
    out[id] = { span, h }
  }
  return out
}

// Valida campo a campo; o que não servir volta ao padrão. Sempre devolve objeto novo.
function parse(raw: unknown): Layout {
  const l = defaultLayout()
  if (!isObj(raw)) return l
  if (raw.cols === 1 || raw.cols === 2 || raw.cols === 3) l.cols = raw.cols as Cols
  if (Array.isArray(raw.order)) l.order = raw.order.filter((x): x is string => typeof x === 'string')
  l.sizes = parseSizes(raw.sizes)
  if (typeof raw.simple === 'boolean') l.simple = raw.simple
  if (raw.tab === 'all' || (typeof raw.tab === 'number' && Number.isInteger(raw.tab))) l.tab = raw.tab
  if (raw.sub === 'all' || raw.sub === 'geral' || (typeof raw.sub === 'number' && Number.isInteger(raw.sub))) l.sub = raw.sub
  return l
}

export function loadLayout(s?: Pick<Storage, 'getItem'>): Layout {
  try {
    const raw = (s ?? window.localStorage).getItem(KEY)
    return raw ? parse(JSON.parse(raw)) : defaultLayout()
  } catch {
    return defaultLayout()
  }
}

export function saveLayout(l: Layout, s?: Pick<Storage, 'setItem'>): void {
  try {
    ;(s ?? window.localStorage).setItem(KEY, JSON.stringify(l))
  } catch {
    // Sem armazenamento (modo privado, cota cheia): o layout só não persiste.
  }
}
