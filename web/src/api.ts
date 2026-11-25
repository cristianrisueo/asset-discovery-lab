// Typed access to the explorer API (spec §13.2, §13.3). Only relative /api/*
// URLs are used: the browser never talks to another origin (spec §14.2).

export interface Owner {
  user_id: string;
  name: string | null;
  group_id: string | null;
  group: string | null;
}

export interface Asset {
  id: string;
  hostname: string;
  kind: string;
  os: string;
  site: string;
  owner: Owner | null;
  version: number;
  changed_at: string;
}

export interface HistoryVersion {
  version: number;
  hostname: string;
  kind: string;
  os: string;
  site: string;
  owner_user_id: string | null;
  changed_at: string;
}

export interface History {
  id: string;
  versions: HistoryVersion[];
}

export interface OsSiteRow {
  os: string;
  site: string;
  assets: number;
}

export interface ChangeRateRow {
  window_start: string;
  window_end: string;
  entity_type: string;
  changes: number;
}

export interface Items<T> {
  items: T[];
}

/** Every call ends in one of these states, so a page never breaks. */
export type Result<T> =
  | { kind: 'ok'; data: T }
  | { kind: 'not_found'; message: string }
  | { kind: 'error'; message: string };

interface ErrorBody {
  error?: string;
  message?: string;
}

async function errorBody(res: Response): Promise<ErrorBody> {
  try {
    return (await res.json()) as ErrorBody;
  } catch {
    return {};
  }
}

export async function getJSON<T>(path: string): Promise<Result<T>> {
  let res: Response;
  try {
    res = await fetch(path, { headers: { Accept: 'application/json' } });
  } catch (e) {
    return { kind: 'error', message: `request failed: ${String(e)}` };
  }
  if (res.status === 404) {
    const body = await errorBody(res);
    return { kind: 'not_found', message: body.message ?? 'not found' };
  }
  if (!res.ok) {
    const body = await errorBody(res);
    const detail = [body.error, body.message].filter(Boolean).join(': ');
    return { kind: 'error', message: `HTTP ${res.status}${detail ? ` (${detail})` : ''}` };
  }
  try {
    return { kind: 'ok', data: (await res.json()) as T };
  } catch (e) {
    return { kind: 'error', message: `invalid JSON: ${String(e)}` };
  }
}

export const api = {
  asset: (id: string) => getJSON<Asset>(`/api/assets/${encodeURIComponent(id)}`),
  history: (id: string) => getJSON<History>(`/api/assets/${encodeURIComponent(id)}/history`),
  osSite: () => getJSON<Items<OsSiteRow>>('/api/stats/os-site'),
  changeRate: (minutes: number) =>
    getJSON<Items<ChangeRateRow>>(`/api/stats/change-rate?minutes=${minutes}`),
};
