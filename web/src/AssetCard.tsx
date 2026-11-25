import type { Asset } from './api';

/** The asset card of spec §14.1. */
export function AssetCard({ asset }: { asset: Asset }) {
  const owner = asset.owner;
  return (
    <section className="card" aria-label={`Asset ${asset.id}`}>
      <h2>{asset.id}</h2>
      <dl>
        <dt>Hostname</dt>
        <dd>{asset.hostname}</dd>
        <dt>Kind</dt>
        <dd>{asset.kind}</dd>
        <dt>OS</dt>
        <dd>{asset.os}</dd>
        <dt>Site</dt>
        <dd>{asset.site}</dd>
        <dt>Owner</dt>
        <dd>
          {owner === null
            ? 'Unassigned'
            : owner.name === null
              ? `${owner.user_id} (user not loaded yet)`
              : `${owner.name} (${owner.user_id})`}
        </dd>
        <dt>Group</dt>
        <dd>
          {owner?.group != null
            ? `${owner.group} (${owner.group_id ?? ''})`
            : (owner?.group_id ?? '—')}
        </dd>
        <dt>Version</dt>
        <dd>{asset.version}</dd>
        <dt>Changed at</dt>
        <dd>
          <time dateTime={asset.changed_at}>{asset.changed_at}</time>
        </dd>
      </dl>
    </section>
  );
}
