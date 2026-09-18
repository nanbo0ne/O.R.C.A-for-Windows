import { useEffect } from "react";
import { flushComposerDrafts, isComposerDraftFlushError } from "./composerDraftPersistence";

export const INSTALLER_DRAFT_FLUSH_REQUEST = "orca:installer-shutdown:flush";
export const INSTALLER_DRAFT_FLUSH_ACK = "orca:installer-shutdown:flushed";

interface InstallerShutdownRuntime {
  EventsOn(name: string, callback: (...data: unknown[]) => void): () => void;
  EventsEmit(name: string, ...data: unknown[]): void;
}

// Leave time for delivery before the backend's one-second acknowledgement limit.
const ATTACHMENT_SETTLE_MS = 800;
const ATTACHMENT_RETRY_MS = 25;

export function installInstallerShutdownFlush(runtime?: Partial<InstallerShutdownRuntime>): () => void {
  if (typeof runtime?.EventsOn !== "function" || typeof runtime.EventsEmit !== "function") return () => {};
  const on = runtime.EventsOn.bind(runtime);
  const emit = runtime.EventsEmit.bind(runtime);
  let disposed = false;
  let request: { id: string; deadline: number; finished: boolean; timer?: ReturnType<typeof setTimeout>; timeout?: ReturnType<typeof setTimeout> } | undefined;

  const acknowledge = (current: NonNullable<typeof request>, saved: boolean) => {
    if (disposed || request !== current || current.finished) return;
    current.finished = true;
    if (current.timer !== undefined) clearTimeout(current.timer);
    if (current.timeout !== undefined) clearTimeout(current.timeout);
    try {
      emit(INSTALLER_DRAFT_FLUSH_ACK, current.id, saved);
    } catch {
      // A torn-down Wails runtime cannot receive an ack; Go still has its timeout.
    }
  };

  const flush = async (current: NonNullable<typeof request>): Promise<void> => {
    if (disposed || request !== current || current.finished) return;
    try {
      await flushComposerDrafts();
      acknowledge(current, true);
    } catch (error) {
      if (disposed || request !== current || current.finished) return;
      if (isComposerDraftFlushError(error) && performance.now() < current.deadline) {
        current.timer = setTimeout(() => { void flush(current); }, Math.min(ATTACHMENT_RETRY_MS, current.deadline - performance.now()));
      } else {
        acknowledge(current, false);
      }
    }
  };

  const off = on(INSTALLER_DRAFT_FLUSH_REQUEST, (id) => {
    if (disposed || typeof id !== "string" || !/^[A-Za-z0-9_-]{1,128}$/.test(id) || request?.id === id) return;
    if (request?.timer !== undefined) clearTimeout(request.timer);
    if (request?.timeout !== undefined) clearTimeout(request.timeout);
    request = { id, deadline: performance.now() + ATTACHMENT_SETTLE_MS, finished: false };
    const current = request;
    current.timeout = setTimeout(() => acknowledge(current, false), ATTACHMENT_SETTLE_MS);
    void flush(request);
  });

  return () => {
    if (disposed) return;
    disposed = true;
    if (request?.timer !== undefined) clearTimeout(request.timer);
    if (request?.timeout !== undefined) clearTimeout(request.timeout);
    off();
  };
}

export function useInstallerShutdown() {
  useEffect(() => {
    if (typeof window === "undefined") return;
    return installInstallerShutdownFlush(window.runtime as Partial<InstallerShutdownRuntime> | undefined);
  }, []);
}
