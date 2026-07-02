import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { StatusPill } from './ui';

describe('StatusPill', () => {
  it('renders the label and the status color', () => {
    render(<StatusPill status="success" label="Healthy" />);
    const pill = screen.getByText('Healthy');
    expect(pill).toBeInTheDocument();
    expect(pill.className).toContain('text-emerald-700');
  });
});
