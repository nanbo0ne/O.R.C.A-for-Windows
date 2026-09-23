import assert from "node:assert/strict";
import { File as NodeFile } from "node:buffer";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import test from "node:test";
import ts from "typescript";
import { DedupIndex } from "../lib/attachDedup";
import { ClipboardPasteArbiter, ClipboardPasteOperation, clipboardFiles, clipboardItemFiles, uniqueClipboardFiles, uniqueClipboardPaths } from "../lib/composerClipboard";

Object.defineProperty(globalThis, "File", { value: NodeFile, configurable: true });

// Execute only the real clipboard callbacks, never mount the app or load a bridge.
const source = readFileSync(new URL("../components/Composer.tsx", import.meta.url), "utf8");
const parsed = ts.createSourceFile("Composer.tsx", source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
const names = new Set([
  "baseName", "fileKey", "clipboardFiles", "clipboardHasImageHint", "isPasteShortcut", "dataURLHash",
  "clearNativeClipboardPasteTimer", "rememberAttachment", "fileDedupKey", "readFileAsDataURL",
  "updateAttachment", "processAttachment", "attachFiles", "attachClipboardFiles",
  "attachNativeClipboardImage", "attachDroppedPaths", "pasteFromContextMenu", "onPaste", "onKeyDown",
]);
const fragments: string[] = [];
function visit(node: ts.Node) {
  if (ts.isFunctionDeclaration(node) && node.name && names.has(node.name.text)) fragments.push(node.getText(parsed));
  if (ts.isVariableDeclaration(node) && names.has(node.name.getText(parsed))) fragments.push(`const ${node.getText(parsed)};`);
  ts.forEachChild(node, visit);
}
visit(parsed);
const callbacks = ts.transpileModule(fragments.join("\n"), { compilerOptions: { target: ts.ScriptTarget.ES2021 } }).outputText;

type Attachment = { status: string; path?: string; source?: File; displayName?: string };
const file = (name = "image.png", bytes = "image", type = "image/png", modified = 1) =>
  new NodeFile([bytes], name, { type, lastModified: modified }) as unknown as File;
const deferred = <T,>() => {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
};
async function flush() {
  for (let i = 0; i < 8; i++) await new Promise<void>((resolve) => setImmediate(resolve));
}
function data(files: File[] = [], items: File[] = files, text = "", types: string[] = []) {
  return {
    files, items: items.map((value) => ({ kind: "file", type: value.type, getAsFile: () => value })),
    types, getData: () => text,
  } as unknown as DataTransfer;
}
function harness(options: {
  native?: () => Promise<string>;
  paths?: () => Promise<string[]>;
  rich?: () => Promise<ClipboardItem[]>;
  readText?: () => Promise<string>;
} = {}) {
  let attachments: Attachment[] = [];
  let pending = 0;
  let nextTimer = 0;
  const timers = new Map<number, () => void>();
  const calls = { native: 0, paths: 0, image: 0, file: 0, dropped: [] as string[], text: [] as string[], warnings: 0 };
  const bindings = {
    ClipboardPasteOperation, clipboardFiles, clipboardItemFiles, uniqueClipboardFiles, uniqueClipboardPaths,
    clipboardPasteArbiterRef: { current: new ClipboardPasteArbiter() },
    console: { warn: () => {} },
    window: {
      setTimeout: (fn: () => void) => { timers.set(++nextTimer, fn); return nextTimer; },
      clearTimeout: (id: number) => timers.delete(id),
    },
    File: NodeFile,
    FileReader: class {
      result = "";
      onload = () => {};
      onerror = () => {};
      readAsDataURL(value: File) {
        void value.arrayBuffer().then((bytes) => {
          this.result = `data:${value.type};base64,${Buffer.from(bytes).toString("base64")}`;
          this.onload();
        }, () => this.onerror());
      }
    },
    // Only synthetic data URLs are accepted. No network or clipboard access.
    fetch: async (url: string) => {
      assert.ok(url.startsWith("data:"));
      return { blob: async () => new Blob([Buffer.from(url.split(",")[1], "base64")]) };
    },
    sha256: async (blob: Blob) => createHash("sha256").update(Buffer.from(await blob.arrayBuffer())).digest("hex"),
    navigator: { clipboard: { read: options.rich, readText: options.readText ?? (async () => "") } },
    app: {
      SaveClipboardImage: async () => { calls.native++; return options.native ? options.native() : `native-${calls.native}.png`; },
      AttachmentDataURL: async () => "data:image/png;base64,bmF0aXZl",
      ReadClipboardFilePaths: async () => { calls.paths++; return options.paths ? options.paths() : []; },
      SavePastedImage: async () => `image-${++calls.image}.png`,
      SavePastedFile: async () => `file-${++calls.file}.bin`,
      AttachDropped: async (path: string) => { calls.dropped.push(path); return { kind: "attachment", path: `saved-${calls.dropped.length}` }; },
    },
    nativeClipboardPasteTimerRef: { current: null as number | null },
    nativeClipboardPasteInFlightRef: { current: false },
    attachmentDedupRef: { current: new DedupIndex() },
    attachmentDedupKeysRef: { current: {} },
    attachmentSequenceRef: { current: 0 },
    setAttachments: (next: (previous: Attachment[]) => Attachment[]) => { attachments = next(attachments); },
    setPendingPaste: (next: (previous: number) => number) => { pending = next(pending); },
    setDragOver: () => {}, addWorkspaceReference: () => {},
    replacePlainTextAtCaret: (value: string) => calls.text.push(value),
    showToast: () => { calls.warnings++; }, t: (value: string) => value,
    isImeKeyEvent: () => false, isYoloToggleShortcut: () => false,
    composingRef: { current: false }, lastCompositionEndAt: { current: 0 }, menuMode: null,
  };
  const api = new Function(...Object.keys(bindings), `${callbacks}\nreturn { onPaste, onKeyDown, attachFiles, pasteFromContextMenu };`)(...Object.values(bindings)) as {
    onPaste: (event: unknown) => void;
    onKeyDown: (event: unknown) => void;
    attachFiles: (files: File[]) => void;
    pasteFromContextMenu: () => Promise<void>;
  };
  return {
    ...api, calls,
    get attachments() { return attachments; },
    get pending() { return pending; },
    key: (key = "v") => api.onKeyDown({ key, ctrlKey: true, altKey: false, metaKey: false, shiftKey: false }),
    fireTimers: () => { for (const [id, fn] of timers) { timers.delete(id); fn(); } },
    paste: (clipboardData: DataTransfer) => {
      let prevented = false;
      api.onPaste({ clipboardData, preventDefault: () => { prevented = true; } });
      return prevented;
    },
  };
}

test("browser files cancel a not-yet-started shortcut fallback", async () => {
  const h = harness(); h.key(); h.paste(data([file()])); h.fireTimers(); await flush();
  assert.equal(h.attachments.length, 1); assert.equal(h.calls.native, 0); assert.equal(h.pending, 0);
});
test("delayed browser event wins against an in-flight native fallback", async () => {
  const native = deferred<string>(); const h = harness({ native: () => native.promise });
  h.key(); h.fireTimers(); h.paste(data([file()])); await flush(); native.resolve("native.png"); await flush();
  assert.equal(h.attachments.length, 1); assert.equal(h.pending, 0);
});
test("browser event after native completion is still the same operation", async () => {
  const h = harness(); h.key(); h.fireTimers(); await flush(); h.paste(data([file()])); await flush();
  assert.equal(h.attachments.length, 1); assert.equal(h.pending, 0);
});
test("two deliberate native pastes retain identical images", async () => {
  const h = harness(); h.key(); h.fireTimers(); await flush(); h.key(); h.fireTimers(); await flush();
  assert.equal(h.attachments.length, 2); assert.equal(h.pending, 0);
});
test("a new shortcut is independent while the previous native read is pending", async () => {
  const first = deferred<string>(); const second = deferred<string>(); let read = 0;
  const h = harness({ native: () => (++read === 1 ? first.promise : second.promise) });
  h.key(); h.fireTimers(); h.key(); h.fireTimers(); second.resolve("second.png"); first.resolve("first.png"); await flush();
  assert.equal(h.calls.native, 2); assert.equal(h.attachments.length, 2); assert.equal(h.pending, 0);
});
test("FileList/items aliases with different metadata attach once", async () => {
  const h = harness();
  h.paste(data([file(), file()], [file("image.png", "image", "image/png", 2)])); await flush();
  assert.equal(h.attachments.length, 1); assert.equal(h.pending, 0);
});
test("equal metadata must not swallow different bytes", async () => {
  const h = harness(); h.paste(data([file("image.png", "AAAA"), file("image.png", "BBBB")])); await flush();
  assert.equal(h.attachments.length, 2);
});
test("separate browser pastes and explicit adds preserve the same File", async () => {
  const h = harness(); const value = file();
  h.paste(data([value])); h.paste(data([value])); h.attachFiles([value]); h.attachFiles([value]); await flush();
  assert.equal(h.attachments.length, 4); assert.equal(h.pending, 0);
});
test("PDF/doc/archive/audio/video bytes are authoritative without duplicate native paths", async () => {
  for (const [name, type] of [["a.pdf", "application/pdf"], ["a.docx", "application/octet-stream"], ["a.zip", "application/zip"], ["a.wav", "audio/wav"], ["a.mp4", "video/mp4"]]) {
    const h = harness({ paths: async () => [`C:/source/alias-${name}`] });
    h.paste(data([file(name, "contents", type)])); await flush();
    assert.equal(h.attachments.length, 1, name); assert.equal(h.calls.paths, 0, name); assert.equal(h.pending, 0, name);
  }
});
test("differently named documents with equal bytes remain distinct", async () => {
  const h = harness(); h.paste(data([file("a.pdf", "same", "application/pdf"), file("b.pdf", "same", "application/pdf")])); await flush();
  assert.equal(h.attachments.length, 2);
});
test("native path aliases deduplicate within a paste, not across pastes", async () => {
  const h = harness({ paths: async () => ["C:\\Folder\\a.pdf", "c:/folder/./a.pdf", "file:///C:/Folder/a.pdf"] });
  h.paste(data()); await flush(); assert.equal(h.attachments.length, 1);
  h.paste(data()); await flush(); assert.equal(h.attachments.length, 2); assert.equal(h.pending, 0);
});
test("native failure leaves the browser representation available", async () => {
  const h = harness({ native: async () => { throw new Error("synthetic native failure"); } });
  h.key(); h.fireTimers(); await flush(); h.paste(data([file()])); await flush();
  assert.equal(h.attachments.length, 1); assert.equal(h.pending, 0);
});
test("plain text stays native and disarms an in-flight image read", async () => {
  const native = deferred<string>(); const h = harness({ native: () => native.promise });
  h.key(); h.fireTimers(); assert.equal(h.paste(data([], [], "editable text", ["image/png"])), false);
  native.resolve("native.png"); await flush(); assert.equal(h.attachments.length, 0); assert.equal(h.pending, 0);
});
test("image hints reuse one native read in the same operation", async () => {
  const native = deferred<string>(); const h = harness({ native: () => native.promise });
  h.key(); h.fireTimers(); h.paste(data([], [], "", ["image/png"])); await flush(); native.resolve("native.png"); await flush();
  assert.equal(h.calls.native, 1); assert.equal(h.attachments.length, 1); assert.equal(h.pending, 0);
});
test("late text removes only that operation's completed native fallback", async () => {
  const h = harness(); h.attachFiles([file("keep.png")]); await flush();
  h.key(); h.fireTimers(); await flush(); assert.equal(h.attachments.length, 2);
  assert.equal(h.paste(data([], [], "text", ["image/png"])), false); await flush();
  assert.equal(h.attachments.length, 1); assert.equal(h.attachments[0].displayName, "keep.png");
});
test("exact pixel matches collapse image encodings but never distinct images", async () => {
  const png = file(); const jpeg = file("image.jpg", "JPEG", "image/jpeg"); const different = file("other.png", "other");
  assert.deepEqual(await uniqueClipboardFiles([png, jpeg, different], async (value) => value === different ? "different" : "same"), [png, different]);
  assert.deepEqual(await uniqueClipboardFiles([png, jpeg], async () => ""), [png, jpeg], "unsupported decoding must retain ambiguous files");
});
test("context menu uses one representation per item and preserves mixed formats", async () => {
  const image = { types: ["image/png", "image/jpeg"], getType: async (type: string) => new Blob([type], { type }) } as unknown as ClipboardItem;
  const pdf = { types: ["application/pdf"], getType: async () => new Blob(["pdf"], { type: "application/pdf" }) } as unknown as ClipboardItem;
  const h = harness({ rich: async () => [image, image, pdf] });
  await h.pasteFromContextMenu(); await flush();
  assert.equal(h.attachments.length, 2); assert.equal(h.calls.native, 0); assert.equal(h.pending, 0);
  await h.pasteFromContextMenu(); await flush(); assert.equal(h.attachments.length, 4);
});
test("unavailable rich representation falls through within the same item", async () => {
  const values = await clipboardItemFiles([{ types: ["image/png", "image/jpeg"], getType: async (type: string) => {
    if (type === "image/png") throw new Error("synthetic unsupported type");
    return new Blob(["jpeg"], { type });
  } } as unknown as ClipboardItem]);
  assert.equal(values.length, 1); assert.equal(values[0].type, "image/jpeg");
});
test("path identity preserves same basenames in different directories and POSIX case", () => {
  assert.equal(uniqueClipboardPaths(["C:/a/report.pdf", "C:/b/report.pdf", "/tmp/A", "/tmp/a"]).length, 4);
  assert.equal(uniqueClipboardPaths(["\\\\server\\share\\a.pdf", "file://server/share/a.pdf"]).length, 1);
});
test("new non-paste key separates a later standalone browser paste", async () => {
  const h = harness(); h.key(); h.fireTimers(); await flush(); h.key("a"); h.paste(data([file()])); await flush();
  assert.equal(h.attachments.length, 2);
});
test("a late multi-file browser payload replaces only its native preview", async () => {
  const h = harness(); h.attachFiles([file("keep.png")]); await flush();
  h.key(); h.fireTimers(); await flush();
  h.paste(data([file("a.png", "first"), file("b.pdf", "second", "application/pdf")])); await flush();
  assert.equal(h.attachments.length, 3);
  assert.deepEqual(h.attachments.map((item) => item.displayName), ["keep.png", "a.png", "b.pdf"]);
});
test("late native file paths replace the single-image fallback without losing files", async () => {
  const h = harness({ paths: async () => ["C:/a.pdf", "C:/b.pdf"] });
  h.key(); h.fireTimers(); await flush(); h.paste(data()); await flush();
  assert.equal(h.attachments.length, 2); assert.equal(h.pending, 0);
});
