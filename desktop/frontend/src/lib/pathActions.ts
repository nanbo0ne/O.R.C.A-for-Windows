// File actions for paths shown in a conversation. The host registers the real
// handlers once, so the markdown renderer stays free of app-wide prop drilling.
export interface PathHandlers {
  open: (path: string) => void;
  reveal: (path: string) => void;
  openWith: (path: string) => void;
  addToChat: (path: string) => void;
}

let handlers: PathHandlers | null = null;

export function registerPathHandlers(next: PathHandlers | null) {
  handlers = next;
}

export function pathHandlers(): PathHandlers | null {
  return handlers;
}

const SCHEME = /^[a-z][a-z0-9+.-]*:/i;

// A link target that points at a local file rather than a web page. Windows
// drive paths, UNC paths, file:// URLs and relative workspace paths all qualify.
export function localPathFromHref(href: string | undefined): string | null {
  const raw = (href ?? "").trim();
  if (!raw) return null;
  if (raw.startsWith("file://")) {
    try {
      return decodeURIComponent(raw.replace(/^file:\/\/\/?/, ""));
    } catch {
      return raw.replace(/^file:\/\/\/?/, "");
    }
  }
  if (SCHEME.test(raw) && !/^[a-z]:[\\/]/i.test(raw)) return null;
  if (/^[a-z]:[\\/]/i.test(raw) || raw.startsWith("\\\\")) return raw;
  if (raw.startsWith("#") || raw.startsWith("/api/")) return null;
  // Relative path with a filename and extension, e.g. desktop/app.go
  if (/^[^\s?#]+\.[A-Za-z0-9]{1,12}(?:[?#].*)?$/.test(raw)) return raw;
  return null;
}
