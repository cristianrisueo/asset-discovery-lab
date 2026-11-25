import { useEffect, useState } from 'react';
import { api, type ChangeRateRow, type Items, type OsSiteRow, type Result } from './api';
import { Status } from './Status';

const MINUTES = 60;
const REFRESH_MS = 15000;

/** The stats page (spec §14.1): OS × site from the MV, and the changes of
 * the last 60 minutes with CSS bars. */
export function StatsPage() {
  const [osSite, setOsSite] = useState<Result<Items<OsSiteRow>> | null>(null);
  const [rate, setRate] = useState<Result<Items<ChangeRateRow>> | null>(null);

  useEffect(() => {
    let cancelled = false;
    const load = () => {
      api.osSite().then((r) => !cancelled && setOsSite(r));
      api.changeRate(MINUTES).then((r) => !cancelled && setRate(r));
    };
    load();
    const timer = setInterval(load, REFRESH_MS);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, []);

  return (
    <div className="page">
      <h2>Assets by OS and site</h2>
      <Section result={osSite} render={(d) => <OsSiteTable rows={d.items} />} />
      <h2>Changes in the last {MINUTES} minutes</h2>
      <Section result={rate} render={(d) => <ChangeRate rows={d.items} />} />
    </div>
  );
}

function Section<T>({
  result,
  render,
}: {
  result: Result<T> | null;
  render: (data: T) => React.ReactNode;
}) {
  if (result === null) return <Status kind="loading">Loading…</Status>;
  if (result.kind === 'not_found') return <Status kind="empty">Not found.</Status>;
  if (result.kind === 'error')
    return <Status kind="error">Could not load: {result.message}</Status>;
  return <>{render(result.data)}</>;
}

export function OsSiteTable({ rows }: { rows: OsSiteRow[] }) {
  if (rows.length === 0) return <Status kind="empty">No data yet.</Status>;
  const sites = [...new Set(rows.map((r) => r.site))].sort();
  const oses = [...new Set(rows.map((r) => r.os))].sort();
  const count = new Map(rows.map((r) => [`${r.os}\u0000${r.site}`, r.assets]));
  return (
    <table className="matrix">
      <thead>
        <tr>
          <th>OS \ site</th>
          {sites.map((s) => (
            <th key={s}>{s}</th>
          ))}
          <th>Total</th>
        </tr>
      </thead>
      <tbody>
        {oses.map((os) => {
          const cells = sites.map((s) => count.get(`${os}\u0000${s}`) ?? 0);
          return (
            <tr key={os}>
              <th scope="row">{os}</th>
              {cells.map((c, i) => (
                <td key={sites[i]}>{c}</td>
              ))}
              <td>{cells.reduce((a, b) => a + b, 0)}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

export function ChangeRate({ rows }: { rows: ChangeRateRow[] }) {
  if (rows.length === 0) return <Status kind="empty">No changes in this period.</Status>;
  const max = Math.max(...rows.map((r) => r.changes), 1);
  return (
    <ul className="bars">
      {rows.map((r) => (
        <li key={`${r.window_start}-${r.entity_type}`}>
          <span className="bar-label">
            {r.window_start.slice(11, 16)} {r.entity_type}
          </span>
          <span className="bar-track">
            <span
              className={`bar bar-${r.entity_type}`}
              style={{ width: `${(r.changes / max) * 100}%` }}
            />
          </span>
          <span className="bar-value">{r.changes}</span>
        </li>
      ))}
    </ul>
  );
}
