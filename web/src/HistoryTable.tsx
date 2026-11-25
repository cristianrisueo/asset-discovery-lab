import { useEffect, useState } from 'react';
import { api, type History, type Result } from './api';
import { Status } from './Status';

/** The version table of an asset, read from the lake (spec §14.1). It reloads
 * when the card sees a new version. */
export function HistoryTable({ id, version }: { id: string; version: number }) {
  const [result, setResult] = useState<Result<History> | null>(null);

  useEffect(() => {
    let cancelled = false;
    api.history(id).then((r) => {
      if (!cancelled) setResult(r);
    });
    return () => {
      cancelled = true;
    };
  }, [id, version]);

  if (result === null) return <Status kind="loading">Loading history…</Status>;
  if (result.kind === 'not_found')
    return <Status kind="empty">No history found for {id} (not found).</Status>;
  if (result.kind === 'error')
    return <Status kind="error">Could not load the history: {result.message}</Status>;
  if (result.data.versions.length === 0) return <Status kind="empty">No versions yet.</Status>;

  return (
    <table className="history">
      <caption>History of {id}</caption>
      <thead>
        <tr>
          <th>Version</th>
          <th>Site</th>
          <th>OS</th>
          <th>Owner</th>
          <th>Hostname</th>
          <th>Kind</th>
          <th>Changed at</th>
        </tr>
      </thead>
      <tbody>
        {result.data.versions.map((v) => (
          <tr key={v.version}>
            <td>{v.version}</td>
            <td>{v.site}</td>
            <td>{v.os}</td>
            <td>{v.owner_user_id ?? 'Unassigned'}</td>
            <td>{v.hostname}</td>
            <td>{v.kind}</td>
            <td>
              <time dateTime={v.changed_at}>{v.changed_at}</time>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
