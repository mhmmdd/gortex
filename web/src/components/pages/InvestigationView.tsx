'use client'

import { useState } from 'react'
import { Icon } from '@/components/primitives/Icon'
import { CaveatBadge } from '@/components/primitives/Caveat'
import { useContracts, useGuards } from '@/lib/hooks'
import {
  INVESTIGATION_FLOW, INVESTIGATION_NOTES, INVESTIGATION_SOURCE_PEEK,
  INVESTIGATION_TIMELINE,
} from '@/lib/seed'

// NOTE: Investigation entities aren't persisted server-side yet — there
// is no `/v1/investigations` endpoint. Until that lands, the flow trace,
// hypothesis, source peek and timeline are seeded (see lib/seed.ts).
// The contracts and guards tiles ARE backed by real /v1/* data.

export function InvestigationView() {
  const [stepIdx, setStepIdx] = useState(3)
  const { data: contracts } = useContracts()
  const { data: guards } = useGuards()

  return (
    <>
      <div className="page-hd">
        <div>
          <div className="hstack" style={{ gap: 8, marginBottom: 4 }}>
            <Icon name="flask" size={14} />
            <span className="mono faint" style={{ fontSize: 11 }}>investigation · demo</span>
            <span className="chip" style={{ color: 'var(--warn)' }}>not persisted</span>
          </div>
          <h1>Email ingest returns 500 intermittently</h1>
          <div className="sub">
            Cross-repo trace · web → core-api → email-worker → worker → tuck_app · pinned by @sam
          </div>
        </div>
      </div>
      <div style={{ overflow: 'auto', flex: 1 }}>
        <div className="inv-grid">
          <div className="inv-tile inv-c-8">
            <div className="tile-hd">
              <Icon name="route" size={12} />
              <span className="ti">Request flow</span>
              <span className="meta">demo · {INVESTIGATION_FLOW.length} steps</span>
            </div>
            <div className="tile-bd">
              {INVESTIGATION_FLOW.map((s, i) => {
                let hop: React.ReactNode = null
                if (i > 0 && INVESTIGATION_FLOW[i - 1].repo !== s.repo) {
                  hop = (
                    <div className="repo-hop">
                      <Icon name="arrowr" size={10} /> crosses {INVESTIGATION_FLOW[i - 1].repo} → {s.repo}
                    </div>
                  )
                }
                return (
                  <div key={s.idx}>
                    {hop}
                    <div
                      className={`flow-step ${s.risk ? 'risk' : ''}`}
                      style={{
                        background: stepIdx === s.idx ? 'var(--accent-soft)' : 'transparent',
                        borderRadius: 4,
                        cursor: 'pointer',
                      }}
                      onClick={() => setStepIdx(s.idx)}
                    >
                      <div className="idx">
                        <span className="no">{s.idx}</span>
                      </div>
                      <div className="body">
                        <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
                          <span className="repo-tag">{s.repo}</span>
                          <span className="where">{s.where}</span>
                          {s.caveat && <CaveatBadge kind={s.caveat} />}
                          {s.risk && <CaveatBadge kind="risk" />}
                        </div>
                        <div className="what">{s.what}</div>
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          </div>

          <div className="inv-tile inv-c-4">
            <div className="tile-hd">
              <Icon name="file" size={12} />
              <span className="ti">Source peek · step {stepIdx}</span>
              <span className="meta mono">demo</span>
            </div>
            <div className="tile-bd">
              <pre className="code" style={{ margin: 0 }}>{INVESTIGATION_SOURCE_PEEK}</pre>
            </div>
          </div>

          <div className="inv-tile inv-c-6">
            <div className="tile-hd">
              <Icon name="history" size={12} />
              <span className="ti">Recent edits</span>
              <span className="meta">demo</span>
            </div>
            <div className="tile-bd">
              {INVESTIGATION_TIMELINE.map((c, i) => (
                <div
                  key={i}
                  style={{
                    display: 'grid',
                    gridTemplateColumns: '60px 1fr 70px',
                    alignItems: 'center',
                    gap: 10,
                    padding: '8px 0',
                    borderBottom: '1px dashed var(--line-1)',
                    fontSize: 12,
                  }}
                >
                  <span className="mono faint" style={{ fontSize: 11 }}>{c.t}</span>
                  <div>
                    <div style={{ marginBottom: 2 }}>{c.msg}</div>
                    <div className="mono faint" style={{ fontSize: 10.5 }}>
                      {c.who} · <span style={{ color: 'var(--fg-3)' }}>{c.hash}</span>
                      {c.risk && <span style={{ marginLeft: 8, color: 'var(--warn)' }}>· guard warn</span>}
                    </div>
                  </div>
                  <span className="tag-dim" style={{ textAlign: 'center' }}>open</span>
                </div>
              ))}
            </div>
          </div>

          <div className="inv-tile inv-c-6">
            <div className="tile-hd">
              <Icon name="plug" size={12} />
              <span className="ti">Contracts (live)</span>
              <span className="meta">{contracts?.length ?? '…'} from /v1/contracts</span>
            </div>
            <div className="tile-bd" style={{ padding: 0 }}>
              <table className="tbl">
                <thead>
                  <tr>
                    <th>Contract</th>
                    <th>Type</th>
                    <th>Consumers</th>
                    <th>Status</th>
                  </tr>
                </thead>
                <tbody>
                  {(contracts ?? []).slice(0, 5).map((c) => (
                    <tr key={c.id}>
                      <td className="mono-cell">{c.name}</td>
                      <td>
                        <span className="tag-dim">{c.kind}</span>
                      </td>
                      <td>
                        <div className="hstack" style={{ gap: 4, flexWrap: 'wrap' }}>
                          {c.consumers.map((r, i) => (
                            <span key={i} className="tag-dim">{r}</span>
                          ))}
                        </div>
                      </td>
                      <td>
                        {c.breaking ? (
                          <CaveatBadge kind="boundary" />
                        ) : (
                          <span className="chip" style={{ color: 'var(--ok)' }}>{c.version || 'ok'}</span>
                        )}
                      </td>
                    </tr>
                  ))}
                  {!contracts?.length && (
                    <tr>
                      <td colSpan={4} className="faint" style={{ padding: 14 }}>No contracts indexed.</td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>

          <div className="inv-tile inv-c-6">
            <div className="tile-hd">
              <Icon name="beaker" size={12} />
              <span className="ti">Guards (live)</span>
              <span className="meta">{guards?.length ?? '…'} from /v1/guards</span>
            </div>
            <div className="tile-bd" style={{ padding: 0 }}>
              <table className="tbl">
                <thead>
                  <tr>
                    <th>Rule</th>
                    <th>Kind</th>
                    <th>Scope</th>
                    <th className="num">Hits</th>
                  </tr>
                </thead>
                <tbody>
                  {(guards ?? []).map((g) => (
                    <tr key={g.id}>
                      <td className="mono-cell">{g.name}</td>
                      <td>
                        <span className="tag-dim">{g.kind}</span>
                      </td>
                      <td className="mono-cell faint">{g.scope}</td>
                      <td className="num">
                        {g.status === 'violated' && <span className="cav risk">{g.hits}</span>}
                        {g.status === 'warn' && <span className="cav deprecated">{g.hits}</span>}
                        {g.status === 'ok' && <span className="faint">0</span>}
                      </td>
                    </tr>
                  ))}
                  {!guards?.length && (
                    <tr>
                      <td colSpan={4} className="faint" style={{ padding: 14 }}>No guards configured.</td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>

          <div className="inv-tile inv-c-12">
            <div className="tile-hd">
              <Icon name="file" size={12} />
              <span className="ti">Notes</span>
              <span className="meta">demo</span>
            </div>
            <div className="tile-bd" style={{ fontSize: 12.5, lineHeight: 1.6 }}>
              <p style={{ margin: '0 0 8px' }}>
                <b>Hypothesis</b> — {INVESTIGATION_NOTES.hypothesis}
              </p>
              <p style={{ margin: '0 0 8px' }}>
                <b>Next</b> — {INVESTIGATION_NOTES.next}
              </p>
            </div>
          </div>
        </div>
      </div>
    </>
  )
}
