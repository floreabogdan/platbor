import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { Markdown } from './Markdown';

describe('Markdown', () => {
  it('renders headings, bold, inline code, and lists', () => {
    render(
      <Markdown
        source={'# Title\n\nSome **bold** and `code`.\n\n- one\n- two'}
      />,
    );
    expect(screen.getByRole('heading', { name: 'Title' })).toBeInTheDocument();
    expect(screen.getByText('bold').tagName).toBe('STRONG');
    expect(screen.getByText('code').tagName).toBe('CODE');
    const items = screen.getAllByRole('listitem');
    expect(items.map((i) => i.textContent)).toEqual(['one', 'two']);
  });

  it('renders a fenced code block verbatim', () => {
    render(<Markdown source={'```\nnpm install x\n```'} />);
    expect(screen.getByText('npm install x')).toBeInTheDocument();
  });

  it('links only safe URLs and renders others as plain text', () => {
    render(<Markdown source={'[ok](https://example.com) [evil](javascript:alert(1))'} />);
    const link = screen.getByRole('link', { name: 'ok' });
    expect(link).toHaveAttribute('href', 'https://example.com');
    expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'));
    // The javascript: link is neutralized — text remains, but no anchor is emitted.
    expect(screen.queryByRole('link', { name: 'evil' })).not.toBeInTheDocument();
    expect(screen.getByText(/evil/)).toBeInTheDocument();
  });

  it('drops images with an unsafe scheme but keeps safe ones', () => {
    const { container } = render(
      <Markdown source={'![bad](javascript:x) ![good](https://example.com/a.png)'} />,
    );
    const imgs = container.querySelectorAll('img');
    expect(imgs).toHaveLength(1);
    expect(imgs[0]).toHaveAttribute('src', 'https://example.com/a.png');
  });

  it('never emits raw HTML from the source (React escapes text)', () => {
    const { container } = render(<Markdown source={'<img src=x onerror=alert(1)> <script>alert(1)</script>'} />);
    expect(container.querySelector('script')).toBeNull();
    // The angle-bracket text is escaped and shown literally, not parsed as markup.
    expect(container.querySelector('img')).toBeNull();
    expect(container.textContent).toContain('onerror=alert(1)');
  });
});
