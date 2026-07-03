import type { JSX, ReactNode } from 'react';

// Markdown renders a safe subset of Markdown to React elements — never to an
// HTML string. Because every text run becomes a React text node, the browser
// escapes it, so untrusted content (a README proxied from a public registry)
// cannot inject markup. The only attributes we emit are href/src, and those are
// passed through a scheme allowlist below. This deliberately trades a few exotic
// Markdown features for a zero-dependency, no-dangerouslySetInnerHTML renderer.

/** safeHref allows only schemes that cannot execute script. */
function safeHref(url: string): string | undefined {
  const u = url.trim();
  // Absolute http(s)/mailto, or a scheme-less relative link — never javascript:,
  // data:, vbscript:, etc.
  if (/^(https?:\/\/|mailto:)/i.test(u)) return u;
  if (/^[^:]*$/.test(u)) return u; // no scheme at all → relative, safe
  return undefined;
}

/** safeImg allows only remote images and inline image data URIs. */
function safeImg(url: string): string | undefined {
  const u = url.trim();
  if (/^https?:\/\//i.test(u)) return u;
  if (/^data:image\//i.test(u)) return u;
  return undefined;
}

// One combined pass over inline syntax: image/link, code span, bold, italic.
const INLINE =
  /(!?)\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)|`([^`]+)`|\*\*([^*]+?)\*\*|__([^_]+?)__|\*([^*\s][^*]*?)\*|_([^_\s][^_]*?)_/g;

function renderInline(text: string, keyBase: string): ReactNode[] {
  const out: ReactNode[] = [];
  let last = 0;
  let m: RegExpExecArray | null;
  let i = 0;
  INLINE.lastIndex = 0;
  while ((m = INLINE.exec(text)) !== null) {
    if (m.index > last) out.push(text.slice(last, m.index));
    const key = `${keyBase}-i${String(i++)}`;
    const linkText = m[2];
    const linkUrl = m[3];
    if (linkText !== undefined && linkUrl !== undefined) {
      // image (m[1] === '!') or link
      if (m[1] === '!') {
        const src = safeImg(linkUrl);
        out.push(src ? <img key={key} src={src} alt={linkText} className="inline max-w-full" /> : linkText);
      } else {
        const href = safeHref(linkUrl);
        out.push(
          href ? (
            <a key={key} href={href} target="_blank" rel="noreferrer noopener" className="text-teal-700 hover:underline">
              {linkText}
            </a>
          ) : (
            linkText
          ),
        );
      }
    } else if (m[4] !== undefined) {
      out.push(
        <code key={key} className="rounded bg-slate-100 px-1 py-0.5 font-mono text-[0.85em] text-slate-800">
          {m[4]}
        </code>,
      );
    } else if (m[5] !== undefined || m[6] !== undefined) {
      out.push(
        <strong key={key} className="font-semibold text-slate-900">
          {m[5] ?? m[6]}
        </strong>,
      );
    } else if (m[7] !== undefined || m[8] !== undefined) {
      out.push(<em key={key}>{m[7] ?? m[8]}</em>);
    }
    last = INLINE.lastIndex;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

const HEADING_CLASS = ['text-2xl', 'text-xl', 'text-lg', 'text-base', 'text-sm', 'text-sm'];

// Block-level parse: walk lines, grouping fenced code, headings, rules, lists,
// blockquotes, and paragraphs. Anything unrecognized falls through as a paragraph.
function renderBlocks(src: string): ReactNode[] {
  const lines = src.replace(/\r\n?/g, '\n').split('\n');
  const at = (n: number) => lines[n] ?? '';
  const blocks: ReactNode[] = [];
  let i = 0;
  let key = 0;
  const nextKey = () => `b${String(key++)}`;

  while (i < lines.length) {
    const line = at(i);

    // Fenced code block.
    if (/^\s*```/.test(line)) {
      const body: string[] = [];
      i++;
      while (i < lines.length && !/^\s*```/.test(at(i))) {
        body.push(at(i));
        i++;
      }
      i++; // consume closing fence
      blocks.push(
        <pre
          key={nextKey()}
          className="overflow-x-auto rounded-lg bg-ink-900 p-3 font-mono text-xs leading-relaxed text-slate-200"
        >
          <code>{body.join('\n')}</code>
        </pre>,
      );
      continue;
    }

    // Blank line.
    if (/^\s*$/.test(line)) {
      i++;
      continue;
    }

    // Horizontal rule.
    if (/^\s*([-*_])(\s*\1){2,}\s*$/.test(line)) {
      blocks.push(<hr key={nextKey()} className="border-slate-200" />);
      i++;
      continue;
    }

    // ATX heading.
    const heading = line.match(/^\s*(#{1,6})\s+(.*)$/);
    if (heading) {
      const level = (heading[1] ?? '#').length;
      const Tag = `h${String(level)}` as keyof JSX.IntrinsicElements;
      const k = nextKey();
      blocks.push(
        <Tag key={k} className={`font-semibold text-slate-900 ${HEADING_CLASS[level - 1] ?? 'text-base'}`}>
          {renderInline(heading[2] ?? '', k)}
        </Tag>,
      );
      i++;
      continue;
    }

    // Blockquote (consecutive `>` lines).
    if (/^\s*>\s?/.test(line)) {
      const body: string[] = [];
      while (i < lines.length && /^\s*>\s?/.test(at(i))) {
        body.push(at(i).replace(/^\s*>\s?/, ''));
        i++;
      }
      const k = nextKey();
      blocks.push(
        <blockquote key={k} className="border-l-2 border-slate-200 pl-3 text-slate-500">
          {renderInline(body.join(' '), k)}
        </blockquote>,
      );
      continue;
    }

    // Lists (unordered or ordered): consume consecutive item lines.
    const ordered = /^\s*\d+\.\s+/.test(line);
    const unordered = /^\s*[-*+]\s+/.test(line);
    if (ordered || unordered) {
      const items: ReactNode[] = [];
      const marker = ordered ? /^\s*\d+\.\s+/ : /^\s*[-*+]\s+/;
      while (i < lines.length && marker.test(at(i))) {
        const item = at(i).replace(marker, '');
        const lk = `l${String(i)}`;
        items.push(
          <li key={lk} className="ml-1">
            {renderInline(item, lk)}
          </li>,
        );
        i++;
      }
      const cls = 'my-1 space-y-1 pl-5 ' + (ordered ? 'list-decimal' : 'list-disc');
      blocks.push(
        ordered ? (
          <ol key={nextKey()} className={cls}>
            {items}
          </ol>
        ) : (
          <ul key={nextKey()} className={cls}>
            {items}
          </ul>
        ),
      );
      continue;
    }

    // Paragraph: gather until a blank line or a block-starting line.
    const para: string[] = [];
    while (i < lines.length && !isBlockStart(at(i))) {
      para.push(at(i));
      i++;
    }
    const k = nextKey();
    blocks.push(
      <p key={k} className="leading-relaxed">
        {renderInline(para.join(' '), k)}
      </p>,
    );
  }

  return blocks;
}

// isBlockStart reports whether a line begins a non-paragraph block, so a
// paragraph stops before it.
function isBlockStart(line: string): boolean {
  return (
    /^\s*$/.test(line) ||
    /^\s*```/.test(line) ||
    /^\s*#{1,6}\s+/.test(line) ||
    /^\s*>\s?/.test(line) ||
    /^\s*\d+\.\s+/.test(line) ||
    /^\s*[-*+]\s+/.test(line)
  );
}

/** Markdown renders README-style text as a safe subset of Markdown. */
export function Markdown({ source, className }: { source: string; className?: string }) {
  return <div className={className}>{renderBlocks(source)}</div>;
}
