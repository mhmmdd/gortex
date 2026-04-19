'use client'

import { useState } from 'react'
import { Icon } from '@/components/primitives/Icon'
import { CaveatBadge } from '@/components/primitives/Caveat'
import { useCaveats } from '@/lib/hooks'
import type { Caveat } from '@/lib/schema'

const TABS = ['all', 'risk', 'deprecated', 'hot', 'unowned', 'cycle', 'boundary'] as const
type Tab = (typeof TABS)[number]

export function CaveatsView() {
  const { data, loading, error, refetch } = useCaveats()
  const [tab, setTab] = useState<Tab>('all')
  const all = data ?? []
  const filtered = tab === 'all' ? all : all.filter((c: Caveat) => c.severity === tab)

  return (
    <>
      <div className="page-hd">
        <div>
          <h1>Caveats</h1>
          <div className="sub">
            {loading
              ? 'Aggregating hotspots, dead code, cycles, guard violations…'
              : `${all.length} landmines · severity-ranked`}
          </div>
        </div>
        <div className="actions">
          <button type="button" className="btn" onClick={refetch}>
            <Icon name="history" size={12} /> Refresh
          </button>
        </div>
      </div>

      <div style={{ padding: '14px 22px 0', borderBottom: '1px solid var(--line-1)' }}>
        <div className="seg" style={{ height: 30, flexWrap: 'wrap' }}>
          {TABS.map((c) => (
            <button
              key={c}
              type="button"
              className={tab === c ? 'active' : ''}
              onClick={() => setTab(c)}
              style={{ textTransform: 'capitalize' }}
            >
              {c}{' '}
              <span className="mono faint" style={{ marginLeft: 6 }}>
                {c === 'all' ? all.length : all.filter((x: Caveat) => x.severity === c).length}
              </span>
            </button>
          ))}
        </div>
      </div>

      {error && (
        <div style={{ padding: 22, color: 'var(--danger)', fontSize: 13 }}>
          Failed to load caveats: {error}
        </div>
      )}

      {!error && all.length === 0 && !loading && (
        <div style={{ padding: 22, color: 'var(--fg-2)', fontSize: 13 }}>
          No caveats detected. Re-index a repository or check that <code>analyze</code> tools are registered.
        </div>
      )}

      <div style={{ padding: 18, overflow: 'auto', display: 'grid', gap: 8 }}>
        {filtered.map((c: Caveat, i: number) => (
          <div
            key={`${c.id}-${i}`}
            className="card"
            style={{ display: 'grid', gridTemplateColumns: '120px 1fr auto', gap: 14, padding: 14, alignItems: 'start' }}
          >
            <div>
              <CaveatBadge kind={c.severity} />
            </div>
            <div>
              <div style={{ fontSize: 13.5, color: 'var(--fg-0)' }}>{c.title}</div>
              <div className="mono faint" style={{ fontSize: 11, marginTop: 2 }}>{c.symbol}</div>
              {c.desc && <div style={{ fontSize: 12, color: 'var(--fg-1)', marginTop: 8 }}>{c.desc}</div>}
            </div>
            <div style={{ textAlign: 'right', fontSize: 11 }}>
              {c.owner && (
                <div className="hstack" style={{ justifyContent: 'flex-end', gap: 6 }}>
                  <Icon name="owner" size={11} />
                  <span className="mono">{c.owner}</span>
                </div>
              )}
              {c.age && <div className="faint mono" style={{ marginTop: 4 }}>{c.age}</div>}
            </div>
          </div>
        ))}
      </div>
    </>
  )
}
