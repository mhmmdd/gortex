'use client'

import { Icon } from '@/components/primitives/Icon'
import { CaveatBadge } from '@/components/primitives/Caveat'
import { useInspector } from '@/lib/inspector'
import { useUsages, useDependencies } from '@/lib/hooks'

export function SymbolInspector() {
  const sym = useInspector((s) => s.sym)
  const setSym = useInspector((s) => s.setSym)
  const usages = useUsages(sym?.id ?? null)
  const deps = useDependencies(sym?.id ?? null)

  if (!sym) {
    return (
      <div style={{ padding: 20, color: 'var(--fg-2)', fontSize: 12.5 }}>
        <div className="section-label" style={{ padding: 0, marginBottom: 10 }}>Inspector</div>
        <div style={{ padding: '40px 0', textAlign: 'center', color: 'var(--fg-3)' }}>
          <Icon name="search" size={18} />
          <div style={{ marginTop: 8 }}>Select a symbol, edge, or flow step</div>
          <div style={{ fontSize: 11, marginTop: 4 }}>Details appear here without leaving the canvas</div>
        </div>
      </div>
    )
  }

  const callerNodes = usages.data?.nodes ?? []
  const calleeNodes = deps.data?.nodes ?? []

  return (
    <div>
      <div className="sym-hd">
        <div className="hstack" style={{ justifyContent: 'space-between' }}>
          <span className="kind">
            <span className={`swatch sw-${sym.kind}`} style={{ marginRight: 6 }} />
            {sym.kind}
          </span>
          <button type="button" className="btn small ghost" onClick={() => setSym(null)}>
            <Icon name="close" size={11} />
          </button>
        </div>
        <div className="name">{sym.name}</div>
        <div className="path">
          {sym.repo} · {sym.file}
        </div>
        {sym.caveats?.length > 0 && (
          <div className="hstack" style={{ marginTop: 8, gap: 4, flexWrap: 'wrap' }}>
            {sym.caveats.map((c) => (
              <CaveatBadge key={c} kind={c} />
            ))}
          </div>
        )}
        <div className="hstack" style={{ gap: 6, marginTop: 10 }}>
          <button type="button" className="btn small">
            <Icon name="file" size={11} /> Open file
          </button>
          <button type="button" className="btn small ghost">
            <Icon name="copy" size={11} /> Copy id
          </button>
          <button type="button" className="btn small ghost">
            <Icon name="pin" size={11} /> Pin
          </button>
        </div>
      </div>

      {sym.sig && (
        <div className="sym-section">
          <div className="sec-ti">Signature</div>
          <pre className="code" style={{ margin: 0 }}>{sym.sig}</pre>
        </div>
      )}

      <div className="sym-section">
        <div className="sec-ti">
          <span>Callers</span>
          <span className="mono faint" style={{ fontSize: 11 }}>
            {usages.loading ? '…' : `${callerNodes.length} sites`}
          </span>
        </div>
        {usages.error && <div className="faint" style={{ fontSize: 11 }}>error: {usages.error}</div>}
        {!usages.loading && callerNodes.length === 0 && (
          <div className="faint" style={{ fontSize: 11 }}>no incoming references</div>
        )}
        {callerNodes.slice(0, 8).map((n) => (
          <button
            type="button"
            key={n.id}
            className="ref"
            style={{ width: '100%', textAlign: 'left' }}
            onClick={() =>
              setSym({
                id: n.id,
                kind: (n.kind as 'function') ?? 'function',
                name: n.name,
                repo: n.repo_prefix ?? '',
                file: `${n.file_path}:${n.start_line ?? 0}`,
                sig: '',
                callers: 0,
                callees: 0,
                community: '',
                caveats: [],
              })
            }
          >
            <span className={`swatch sw-${n.kind ?? 'function'}`} />
            <span className="where">{n.name}</span>
            <span className="count">{n.repo_prefix ?? ''}</span>
          </button>
        ))}
      </div>

      <div className="sym-section">
        <div className="sec-ti">
          <span>Calls</span>
          <span className="mono faint" style={{ fontSize: 11 }}>
            {deps.loading ? '…' : `${calleeNodes.length} symbols`}
          </span>
        </div>
        {deps.error && <div className="faint" style={{ fontSize: 11 }}>error: {deps.error}</div>}
        {!deps.loading && calleeNodes.length === 0 && (
          <div className="faint" style={{ fontSize: 11 }}>no outgoing dependencies</div>
        )}
        {calleeNodes.slice(0, 8).map((n) => (
          <button
            type="button"
            key={n.id}
            className="ref"
            style={{ width: '100%', textAlign: 'left' }}
            onClick={() =>
              setSym({
                id: n.id,
                kind: (n.kind as 'function') ?? 'function',
                name: n.name,
                repo: n.repo_prefix ?? '',
                file: `${n.file_path}:${n.start_line ?? 0}`,
                sig: '',
                callers: 0,
                callees: 0,
                community: '',
                caveats: [],
              })
            }
          >
            <span className={`swatch sw-${n.kind ?? 'method'}`} />
            <span className="where">{n.name}</span>
            <span className="count">{n.repo_prefix ?? ''}</span>
          </button>
        ))}
      </div>

      {sym.community && (
        <div className="sym-section">
          <div className="sec-ti">Community</div>
          <div style={{ fontSize: 12.5 }}>
            <div className="mono" style={{ color: 'var(--fg-0)' }}>{sym.community}</div>
          </div>
        </div>
      )}
    </div>
  )
}
