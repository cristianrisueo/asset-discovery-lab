import { type MouseEvent, useEffect, useState } from 'react';
import { AssetPage } from './AssetPage';
import { StatsPage } from './StatsPage';

type Page = 'asset' | 'stats';

const pageOf = (path: string): Page => (path.startsWith('/stats') ? 'stats' : 'asset');

/** Two client routes, `/` (asset) and `/stats`; nginx falls back to
 * index.html for both (spec §14.3). */
export function App() {
  const [page, setPage] = useState<Page>(() => pageOf(window.location.pathname));

  useEffect(() => {
    const onPop = () => setPage(pageOf(window.location.pathname));
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, []);

  const go = (next: Page) => (e: MouseEvent) => {
    e.preventDefault();
    window.history.pushState(null, '', next === 'stats' ? '/stats' : '/');
    setPage(next);
  };

  return (
    <>
      <header>
        <h1>asset-discovery-lab</h1>
        <nav>
          <a href="/" aria-current={page === 'asset' ? 'page' : undefined} onClick={go('asset')}>
            Asset
          </a>
          <a
            href="/stats"
            aria-current={page === 'stats' ? 'page' : undefined}
            onClick={go('stats')}
          >
            Stats
          </a>
        </nav>
      </header>
      <main>{page === 'stats' ? <StatsPage /> : <AssetPage />}</main>
    </>
  );
}
