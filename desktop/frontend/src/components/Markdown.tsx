import { memo, useDeferredValue, useState } from "react";
import ReactMarkdown from "react-markdown";
import type { Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import rehypeKatex from "rehype-katex";
import "katex/dist/katex.min.css";
import { CodeViewer } from "./CodeViewer";
import { ContextMenu, type ContextMenuItem, type ContextMenuPoint } from "./ContextMenu";
import { normalizeMath } from "./mathNormalize";
import { openExternal } from "../lib/bridge";
import { localPathFromHref, pathHandlers } from "../lib/pathActions";

// A link that points at a workspace file gets file actions instead of a browser
// open: left click opens it, right click offers where and how.
function MarkdownLink({ href, children }: { href?: string; children?: React.ReactNode }) {
  const [menu, setMenu] = useState<ContextMenuPoint | null>(null);
  const path = localPathFromHref(href);
  const handlers = path ? pathHandlers() : null;

  if (!path || !handlers) {
    return (
      <a
        href={href}
        onClick={(e) => {
          e.preventDefault();
          if (href) openExternal(href);
        }}
        onAuxClick={(e) => {
          e.preventDefault();
          if (href) openExternal(href);
        }}
        onMouseDown={(e) => {
          if (e.button === 1) e.preventDefault();
        }}
      >
        {children}
      </a>
    );
  }

  const items: ContextMenuItem[] = [
    { key: "open", label: "打开文件", onSelect: () => handlers.open(path) },
    { key: "reveal", label: "在文件夹中显示", onSelect: () => handlers.reveal(path) },
    { key: "with", label: "选择打开方式", onSelect: () => handlers.openWith(path) },
    { type: "separator", key: "sep" },
    { key: "chat", label: "添加到对话", onSelect: () => handlers.addToChat(path) },
  ];

  return (
    <>
      <a
        href={href}
        className="md-file-link"
        title={path}
        onClick={(e) => {
          e.preventDefault();
          handlers.open(path);
        }}
        onContextMenu={(e) => {
          e.preventDefault();
          setMenu({ left: e.clientX, top: e.clientY });
        }}
      >
        {children}
      </a>
      <ContextMenu open={menu !== null} point={menu} items={items} onClose={() => setMenu(null)} ariaLabel="文件操作" />
    </>
  );
}

const components: Components = {
  pre: ({ children }) => <>{children}</>,
  code: ({ className, children }) => {
    const text = String(children ?? "");
    const match = /language-([\w-]+)/.exec(className ?? "");
    const isBlock = match !== null || text.includes("\n");
    if (isBlock) {
      return <CodeViewer value={text.replace(/\n$/, "")} language={match?.[1]} maxHeight={360} />;
    }
    return <code className="md-code">{children}</code>;
  },
  a: ({ href, children }) => <MarkdownLink href={href}>{children}</MarkdownLink>,
};

export const Markdown = memo(function Markdown({
  text,
  showCursor,
}: {
  text: string;
  showCursor?: boolean;
}) {
  // Controller events are already frame-batched; do not delay each fragment again.
  const deferred = useDeferredValue(text);

  return (
    <div className="md">
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkMath]}
        rehypePlugins={[rehypeKatex]}
        components={components}
      >
        {normalizeMath(deferred)}
      </ReactMarkdown>
      {showCursor && <span className="cursor" data-streaming-cursor="true" />}
    </div>
  );
});
