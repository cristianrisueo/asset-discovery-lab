import type { Asset } from './api';

/** The §13.3 Asset example. */
export const lt0042: Asset = {
  id: 'LT-0042',
  hostname: 'lt-0042.acme.local',
  kind: 'LAPTOP',
  os: 'Windows 11',
  site: 'Madrid',
  owner: { user_id: 'U-0117', name: 'Ana Ruiz', group_id: 'G-03', group: 'Ventas' },
  version: 7,
  changed_at: '2026-11-25T10:14:03.120Z',
};
