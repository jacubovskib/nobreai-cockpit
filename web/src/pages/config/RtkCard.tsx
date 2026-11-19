import { useState } from 'react'
import { useRtk, useRtkApply, useRtkPreview } from '../../api/queries'
import { ErrorLine, btn, btnPrimary, muted, panel } from '../card/shared'

const REPO_URL = 'https://github.com/rtk-ai/rtk'
const INSTALL_CMD = `cargo install --git ${REPO_URL} --branch master rtk`

function InstallHint() {
  const [msg, setMsg] = useState('')
  async function copy() {
    try {
      await navigator.clipboard.writeText(INSTALL_CMD)
      setMsg('Comando copiado.')
    } catch {
      setMsg('Não foi possível copiar automaticamente. Selecione o comando e copie.')
    }
  }
  return (
    <>
      <p style={{ ...muted, margin: 0 }}>Instale com:</p>
      <div style={{ display: 'flex', gap: 8, alignItems: 'center', justifyContent: 'space-between', padding: '8px 12px', borderRadius: 8, border: '1px solid var(--border)' }}>
        <code className="mono" style={{ wordBreak: 'break-all' }}>{INSTALL_CMD}</code>
        <button type="button" style={btn} aria-label="Copiar comando" onClick={copy}>Copiar</button>
      </div>
      {msg && <p role="status" style={{ ...muted, margin: 0 }}>{msg}</p>}
    </>
  )
}

export function RtkCard() {
  const status = useRtk()
  const apply = useRtkApply()
  const [pending, setPending] = useState<'activate' | 'deactivate' | null>(null)
  const preview = useRtkPreview(pending)
  const s = status.data
  const confirm = () => {
    if (pending) apply.mutate(pending, { onSuccess: () => setPending(null) })
  }
  return (
    <article aria-label="RTK" style={{ ...panel, background: 'var(--bg)', padding: 14 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
        <h3 style={{ fontSize: 15 }}>RTK</h3>
        {s?.installed && <span className="badge">{s.active ? 'Ativo' : 'Inativo'}</span>}
      </div>
      <p style={{ ...muted, margin: 0 }}>Comprime a saída dos comandos antes de chegar ao agente, para gastar menos tokens.</p>
      {status.isError && <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>Não foi possível ler o estado do RTK.</p>}
      {status.isPending && <p className="placeholder">Detectando…</p>}
      {s && !s.installed && (
        <>
          <strong style={{ fontWeight: 600 }}>RTK não instalado</strong>
          {s.active && (
            <p role="alert" style={{ margin: 0, color: 'var(--warn)' }}>O hook do RTK continua no settings.json, mas o binário sumiu: reinstale ou remova o hook.</p>
          )}
          <InstallHint />
          <p style={{ ...muted, margin: 0 }}>
            Projeto: <a href={REPO_URL} target="_blank" rel="noreferrer">{REPO_URL}</a>
          </p>
        </>
      )}
      {s?.installed && (
        <>
          <p style={{ ...muted, margin: 0 }}>{s.path} · {s.version}</p>
          {s.detail && <p style={{ ...muted, margin: 0, fontSize: 12 }}>{s.detail}</p>}
          {!pending && (
            <button type="button" style={s.active ? btn : btnPrimary} onClick={() => { apply.reset(); setPending(s.active ? 'deactivate' : 'activate') }}>
              {s.active ? 'Desativar' : 'Ativar no Claude Code'}
            </button>
          )}
        </>
      )}
      {pending && (
        <div role="group" aria-label="Confirmar mudança do RTK" style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          <p style={{ ...muted, margin: 0 }}>O que vai mudar no settings do Claude Code:</p>
          {preview.isPending && <p className="placeholder">Gerando prévia…</p>}
          {preview.data && <pre className="mono" style={{ margin: 0, padding: 10, borderRadius: 8, border: '1px solid var(--border)', whiteSpace: 'pre-wrap', fontSize: 12 }}>{preview.data.output}</pre>}
          <ErrorLine error={preview.error} />
          <div style={{ display: 'flex', gap: 8 }}>
            <button type="button" style={btnPrimary} disabled={apply.isPending || !preview.data} onClick={confirm}>Confirmar</button>
            <button type="button" style={btn} disabled={apply.isPending} onClick={() => setPending(null)}>Cancelar</button>
          </div>
        </div>
      )}
      <ErrorLine error={apply.error} />
      {apply.data && (
        <div role="status" style={{ ...muted, fontSize: 13 }}>
          {apply.data.output && <pre className="mono" style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{apply.data.output}</pre>}
          {apply.data.backup && <div>Cópia do settings em <code>{apply.data.backup}</code></div>}
        </div>
      )}
    </article>
  )
}
