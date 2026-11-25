import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { AssetCard } from './AssetCard';
import { lt0042 } from './fixtures';

describe('AssetCard', () => {
  it('renders every field of the asset from a fixture', () => {
    render(<AssetCard asset={lt0042} />);
    const card = screen.getByRole('region', { name: 'Asset LT-0042' });
    expect(card).toHaveTextContent('LT-0042');
    expect(card).toHaveTextContent('lt-0042.acme.local');
    expect(card).toHaveTextContent('LAPTOP');
    expect(card).toHaveTextContent('Windows 11');
    expect(card).toHaveTextContent('Madrid');
    expect(card).toHaveTextContent('Ana Ruiz (U-0117)');
    expect(card).toHaveTextContent('Ventas (G-03)');
    expect(card).toHaveTextContent('7');
    expect(card).toHaveTextContent('2026-11-25T10:14:03.120Z');
  });

  it('shows an unassigned asset', () => {
    render(<AssetCard asset={{ ...lt0042, owner: null }} />);
    expect(screen.getByText('Unassigned')).toBeInTheDocument();
  });

  it('shows an owner whose user row has not arrived yet', () => {
    render(
      <AssetCard
        asset={{ ...lt0042, owner: { user_id: 'U-0117', name: null, group_id: null, group: null } }}
      />,
    );
    expect(screen.getByText('U-0117 (user not loaded yet)')).toBeInTheDocument();
  });
});
