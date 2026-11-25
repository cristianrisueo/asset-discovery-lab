import type { ReactNode } from 'react';

/** Loading, empty and error states, always visible (spec §14.2). */
export function Status({
  kind,
  children,
}: {
  kind: 'loading' | 'empty' | 'error';
  children: ReactNode;
}) {
  return (
    <p className={`status status-${kind}`} role={kind === 'error' ? 'alert' : 'status'}>
      {children}
    </p>
  );
}
