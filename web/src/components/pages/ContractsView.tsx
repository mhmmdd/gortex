'use client'

import { Icon } from '@/components/primitives/Icon'
import { CaveatBadge } from '@/components/primitives/Caveat'
import { useContracts } from '@/lib/hooks'

export function ContractsView() {
  const { data, loading, error, refetch } = useContracts()
  const contracts = data ?? []
  const breaking = contracts.filter((c) => c.breaking).length

  return (
    <>
      <div className="page-hd">
        <div>
          <h1>Contracts</h1>
          <div className="sub">
            {loading
              ? 'Loading detected contracts…'
              : `${contracts.length} API/event boundaries · ${breaking} breaking`}
          </div>
        </div>
        <div className="actions">
          <button type="button" className="btn" onClick={refetch}>
            <Icon name="history" size={12} /> Refresh
          </button>
        </div>
      </div>

      {error && (
        <div style={{ padding: 22, color: 'var(--danger)', fontSize: 13 }}>
          Failed to load contracts: {error}
        </div>
      )}

      {!error && contracts.length === 0 && !loading && (
        <div style={{ padding: 22, color: 'var(--fg-2)', fontSize: 13 }}>
          No contracts detected. Make sure the indexer ran on a repository that exposes HTTP, gRPC, or event topics.
        </div>
      )}

      {contracts.length > 0 && (
        <div style={{ padding: '18px 22px', overflow: 'auto' }}>
          <div style={{ display: 'grid', gap: 10 }}>
            {contracts.map((c) => (
              <div key={c.id} className="card">
                <div
                  style={{
                    display: 'grid',
                    gridTemplateColumns: '28px 1fr auto',
                    gap: 14,
                    padding: 14,
                    alignItems: 'center',
                  }}
                >
                  <div
                    style={{
                      width: 28,
                      height: 28,
                      borderRadius: 6,
                      background:
                        c.kind === 'EVENT'
                          ? 'oklch(0.78 0.14 300 / 0.18)'
                          : c.kind === 'URL'
                          ? 'oklch(0.82 0.15 80 / 0.18)'
                          : 'oklch(0.82 0.14 45 / 0.18)',
                      color:
                        c.kind === 'EVENT' ? 'var(--violet)' : c.kind === 'URL' ? 'var(--warn)' : 'var(--k-contract)',
                      display: 'grid',
                      placeItems: 'center',
                      fontFamily: 'JetBrains Mono',
                      fontSize: 10,
                      fontWeight: 600,
                    }}
                  >
                    {c.kind === 'EVENT' ? 'EV' : c.kind === 'URL' ? 'URL' : 'API'}
                  </div>
                  <div>
                    <div className="hstack" style={{ gap: 8, flexWrap: 'wrap' }}>
                      <span className="mono" style={{ fontSize: 14, color: 'var(--fg-0)' }}>{c.name}</span>
                      {c.breaking && <CaveatBadge kind="boundary" />}
                      {c.version && <span className="chip">{c.version}</span>}
                    </div>
                    <div className="hstack" style={{ gap: 10, marginTop: 6, fontSize: 11.5, color: 'var(--fg-2)', flexWrap: 'wrap' }}>
                      <span>
                        Produced by <span className="tag-dim">{c.producer || 'unknown'}</span>
                      </span>
                      {c.consumers.length > 0 && (
                        <>
                          <span>→</span>
                          <span className="hstack" style={{ gap: 4 }}>
                            consumed by{' '}
                            {c.consumers.map((r) => (
                              <span key={r} className="tag-dim">{r}</span>
                            ))}
                          </span>
                        </>
                      )}
                      <span className="faint">· {c.callers} call sites</span>
                    </div>
                  </div>
                  <div className="hstack" style={{ gap: 6 }}>
                    <button type="button" className="btn small ghost">
                      <Icon name="graph" size={11} /> Trace
                    </button>
                    <button type="button" className="btn small">
                      <Icon name="file" size={11} /> Schema
                    </button>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </>
  )
}
