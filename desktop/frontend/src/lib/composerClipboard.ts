import { sha256 } from "./attachDedup";

// The fallback timer and its possibly late browser event share one operation.
// Independent shortcuts and menu pastes never share this state.
export class ClipboardPasteOperation {
  private claimed = false;
  private nativeRead?: Promise<boolean>;
  private discardNative?: () => void;

  get handled(): boolean { return this.claimed; }
  get browserHandled(): boolean { return this.claimed && !this.discardNative; }

  claim(discardNative?: () => void): boolean {
    if (this.claimed) return false;
    this.claimed = true;
    this.discardNative = discardNative;
    return true;
  }

  useText(): void {
    this.discardNative?.();
    this.discardNative = undefined;
    this.claimed = true;
  }

  claimBrowser(): boolean {
    if (this.browserHandled) return false;
    // A browser FileList can contain more than the native image preview.
    this.useText();
    return true;
  }

  readNative(read: () => Promise<boolean>): Promise<boolean> {
    if (this.handled) return Promise.resolve(true);
    return this.nativeRead ??= read();
  }
}

export class ClipboardPasteArbiter {
  private shortcut?: ClipboardPasteOperation;

  startShortcut(): ClipboardPasteOperation {
    return this.shortcut = new ClipboardPasteOperation();
  }

  browserPaste(): ClipboardPasteOperation {
    const operation = this.shortcut ?? new ClipboardPasteOperation();
    this.endShortcut();
    return operation;
  }

  endShortcut(): void { this.shortcut = undefined; }
}

export function clipboardFiles(data: DataTransfer): File[] {
  const files = new Set(Array.from(data.files));
  for (const item of Array.from(data.items)) {
    if (item.kind !== "file") continue;
    const file = item.getAsFile();
    if (file) files.add(file);
  }
  return [...files];
}

// Do not equate an animation with its first frame or use lossy thumbnails.
// Unsupported/large images retain their byte identity.
export async function clipboardImageFingerprint(file: File): Promise<string> {
  if (!/^image\/(png|jpeg|bmp|x-ms-bmp)$/i.test(file.type) || typeof createImageBitmap !== "function") return "";
  let bitmap: ImageBitmap | undefined;
  try {
    if (file.type.toLowerCase() === "image/png") {
      const bytes = new Uint8Array(await file.arrayBuffer());
      const view = new DataView(bytes.buffer);
      for (let offset = 8; offset + 12 <= bytes.length;) {
        if (String.fromCharCode(...bytes.subarray(offset + 4, offset + 8)) === "acTL") return "";
        offset += 12 + view.getUint32(offset);
      }
    }
    bitmap = await createImageBitmap(file);
    if (bitmap.width * bitmap.height > 4_000_000) return "";
    const canvas = document.createElement("canvas");
    canvas.width = bitmap.width;
    canvas.height = bitmap.height;
    const context = canvas.getContext("2d");
    if (!context) return "";
    context.drawImage(bitmap, 0, 0);
    const pixels = context.getImageData(0, 0, bitmap.width, bitmap.height).data;
    const hash = await sha256(new Blob([new Uint8Array(pixels)]));
    return hash ? `${bitmap.width}:${bitmap.height}:${hash}` : "";
  } catch {
    return "";
  } finally {
    bitmap?.close();
  }
}

export async function clipboardItemFiles(items: ClipboardItem[]): Promise<File[]> {
  const files: File[] = [];
  const extensions: Record<string, string> = {
    "image/jpeg": "jpg", "application/pdf": "pdf", "application/zip": "zip",
    "application/vnd.openxmlformats-officedocument.wordprocessingml.document": "docx",
    "application/octet-stream": "bin",
  };
  for (const item of items) {
    // MIME variants within one ClipboardItem describe one logical item.
    const types = item.types.filter((type) => /^(image|application|audio|video)\//i.test(type));
    for (const type of types) {
      try {
        const blob = await item.getType(type);
        const ext = extensions[type] ?? type.split("/")[1].replace(/[^a-z0-9]/gi, "");
        files.push(new File([blob], `clipboard-${files.length + 1}.${ext || "bin"}`, { type }));
        break;
      } catch { /* Try another representation of this item. */ }
    }
  }
  return files;
}

export async function uniqueClipboardFiles(
  files: File[],
  imageFingerprint = clipboardImageFingerprint,
): Promise<File[]> {
  files = [...new Set(files)];
  if (files.length < 2) return files;
  const accepted: File[] = [];
  const contents: { name: string; image: boolean; bytes: Uint8Array }[] = [];
  const images = new Set<string>();
  const imageCount = files.filter((file) => file.type.toLowerCase().startsWith("image/")).length;
  for (const file of files) {
    const image = file.type.toLowerCase().startsWith("image/");
    try {
      const bytes = new Uint8Array(await file.arrayBuffer());
      const duplicate = contents.some((prior) =>
        (image && prior.image || file.name === prior.name) && prior.bytes.length === bytes.length
        && prior.bytes.every((byte, index) => byte === bytes[index]));
      if (duplicate) continue;
      const pixels = image && imageCount > 1 ? await imageFingerprint(file) : "";
      if (pixels && images.has(pixels)) continue;
      if (pixels) images.add(pixels);
      contents.push({ name: file.name, image, bytes });
    } catch {
      // Keep unreadable files so the attachment error/retry UI owns them.
    }
    accepted.push(file);
  }
  return accepted;
}

function clipboardPathKey(path: string): string {
  let value = path;
  if (/^file:/i.test(value)) {
    try {
      const url = new URL(value);
      value = (url.hostname ? `//${url.hostname}` : "") + decodeURIComponent(url.pathname);
      if (/^\/[a-z]:\//i.test(value)) value = value.slice(1);
    } catch { /* Let attachment error handling own malformed paths. */ }
  }
  const windows = /^[a-z]:[\\/]|^\\\\|^\/\//i.test(value);
  if (windows) value = value.replace(/\\/g, "/").toLowerCase();
  // Do not resolve '..' or symlinks without filesystem knowledge.
  return value.replace(/\/\.\//g, "/");
}

export function uniqueClipboardPaths(paths: string[]): string[] {
  const seen = new Set<string>();
  return paths.filter((path) => {
    const key = clipboardPathKey(path);
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}
