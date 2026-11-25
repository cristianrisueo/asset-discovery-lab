import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { AssetPage } from './AssetPage';
import { lt0042 } from './fixtures';

function respond(status: number, body: unknown) {
  const fetchMock = vi.fn(
    async () =>
      new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': 'application/json' },
      }),
  );
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

describe('AssetPage', () => {
  it('prefills LT-0042 and shows its card', async () => {
    const fetchMock = respond(200, lt0042);
    render(<AssetPage />);
    expect(screen.getByLabelText('Asset id')).toHaveValue('LT-0042');
    expect(await screen.findByText('lt-0042.acme.local')).toBeInTheDocument();
    expect(screen.getByText(/Last updated/)).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith('/api/assets/LT-0042', expect.anything());
  });

  it('shows the error state when the explorer fails', async () => {
    respond(503, { error: 'upstream_unavailable', message: 'Doris is down' });
    render(<AssetPage />);
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('HTTP 503');
    expect(alert).toHaveTextContent('upstream_unavailable: Doris is down');
  });

  it('shows "not found" for a 404, not a broken page', async () => {
    const fetchMock = respond(404, { error: 'not_found', message: 'asset LT-9999 not found' });
    render(<AssetPage />);
    fireEvent.change(screen.getByLabelText('Asset id'), { target: { value: 'lt-9999' } });
    fireEvent.click(screen.getByRole('button', { name: 'Search' }));
    expect(await screen.findByText('Asset LT-9999 not found.')).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith('/api/assets/LT-9999', expect.anything());
  });

  it('shows the error state when the network fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new TypeError('Failed to fetch');
      }),
    );
    render(<AssetPage />);
    expect(await screen.findByRole('alert')).toHaveTextContent('request failed');
  });
});
