import { type FormEvent, useEffect, useState } from 'react';
import { api, type Asset, type Result } from './api';
import { AssetCard } from './AssetCard';
import { HistoryTable } from './HistoryTable';
import { Status } from './Status';

export const DEFAULT_ASSET = 'LT-0042';
const REFRESH_MS = 5000;

interface Loaded {
  id: string;
  result: Result<Asset>;
  at: Date;
}

/** The default page (spec §14.1): search, live asset card and history. */
export function AssetPage({ initialId = DEFAULT_ASSET }: { initialId?: string }) {
  const [input, setInput] = useState(initialId);
  const [id, setId] = useState(initialId);
  const [loaded, setLoaded] = useState<Loaded | null>(null);
  const [showHistory, setShowHistory] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const load = () =>
      api.asset(id).then((result) => {
        if (!cancelled) setLoaded({ id, result, at: new Date() });
      });
    load();
    const timer = setInterval(load, REFRESH_MS);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [id]);

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    const next = input.trim().toUpperCase();
    if (next !== '') setId(next);
  };

  // Results of a previous search are not shown for the new id.
  const current = loaded?.id === id ? loaded : null;

  return (
    <div className="page">
      <form className="search" onSubmit={onSubmit} role="search">
        <label htmlFor="asset-id">Asset id</label>
        <input
          id="asset-id"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder="LT-0042"
          autoComplete="off"
        />
        <button type="submit">Search</button>
      </form>

      {current === null && <Status kind="loading">Loading {id}…</Status>}
      {current?.result.kind === 'not_found' && <Status kind="empty">Asset {id} not found.</Status>}
      {current?.result.kind === 'error' && (
        <Status kind="error">
          Could not load {id}: {current.result.message}
        </Status>
      )}
      {current?.result.kind === 'ok' && (
        <>
          <AssetCard asset={current.result.data} />
          <p className="updated">
            Last updated {current.at.toLocaleTimeString()} · refreshes every {REFRESH_MS / 1000} s
          </p>
          <button type="button" onClick={() => setShowHistory((s) => !s)}>
            {showHistory ? 'Hide history' : 'History'}
          </button>
          {showHistory && <HistoryTable id={id} version={current.result.data.version} />}
        </>
      )}
    </div>
  );
}
