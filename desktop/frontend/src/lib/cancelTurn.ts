import type { CancelAck, TurnStatus } from "./types";

interface RunningTurnSnapshot {
  running: boolean;
  turnEpoch: number;
  currentTurnId?: string;
}

export function startTurnStatusPolling(options: {
  tabs: () => Iterable<string>;
  state: (tabId: string) => RunningTurnSnapshot | undefined;
  pending: (tabId: string) => boolean;
  status: (tabId: string) => Promise<TurnStatus>;
  update: (tabId: string, status: TurnStatus, turnEpoch: number) => void;
}): () => void {
  let disposed = false;
  const inFlight = new Set<string>();
  const poll = async (tabId: string) => {
    const before = options.state(tabId);
    if (!before?.running || options.pending(tabId) || inFlight.has(tabId)) return;
    inFlight.add(tabId);
    try {
      const status = await options.status(tabId);
      const current = options.state(tabId);
      if (disposed || !current?.running || options.pending(tabId) || current.turnEpoch !== before.turnEpoch) return;
      if (before.currentTurnId && before.currentTurnId !== current.currentTurnId) return;
      if (!status.turnId || (current.currentTurnId && current.currentTurnId !== status.turnId)) return;
      // Queue aliases are not agent identities. Wait for admission, an actual
      // turn status, or the cancellation acknowledgement that resolves them.
      if (status.running && status.turnId.startsWith("queued-")) return;
      options.update(tabId, status, before.turnEpoch);
    } catch {
      // A failed read is not idle. Retry on the next tick.
    } finally {
      inFlight.delete(tabId);
    }
  };
  const tick = () => {
    if (disposed) return;
    for (const tabId of options.tabs()) void poll(tabId);
    timer = setTimeout(tick, 500);
  };
  let timer = setTimeout(tick, 500);
  return () => { disposed = true; clearTimeout(timer); };
}

export async function cancelAndObserve(options: {
  turnId?: string;
  admission?: Promise<void>;
  status: () => Promise<TurnStatus>;
  cancel: (turnId: string) => Promise<CancelAck>;
  current: () => boolean;
  update: (status: TurnStatus, replacedTurnId?: string) => void;
  slow: () => void;
  error: (error: unknown) => void;
  delay?: () => Promise<void>;
  now?: () => number;
}): Promise<void> {
  const now = options.now ?? Date.now;
  const started = now();
  const delay = options.delay ?? (() => new Promise<void>((resolve) => setTimeout(resolve, 500)));
  const bounded = <T,>(promise: Promise<T>): Promise<T> => new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("Stop status request timed out")), 5000);
    promise.then(resolve, reject).finally(() => clearTimeout(timer));
  });
  let target = options.turnId;
  let warned = false;
  const warn = () => {
    if (!warned && options.current()) { warned = true; options.slow(); }
  };
  const slowTimer = setTimeout(warn, 5000);
  try {
    // Submit may still be crossing the bridge. Never report idle or cancel a
    // different turn while waiting for this submission to be admitted.
    await options.admission?.catch(() => {});
    if (!options.current()) return;
    try {
      const status = await bounded(options.status());
      if (!options.current()) return;
      if (!status.running) {
        if (target && status.turnId && target !== status.turnId) return;
        options.update(status);
        return;
      }
      target ||= status.turnId;
      if (!target) throw new Error("Current turn identity is unavailable");
      const ack = await bounded(options.cancel(target));
      if (!options.current()) return;
      if (!ack.accepted && ack.turnId && ack.turnId !== target) {
        options.error(new Error("The selected turn has already ended"));
        return;
      }
      const replacedTurnId = ack.accepted ? target : undefined;
      if (ack.accepted && ack.turnId) target = ack.turnId;
      options.update(ack, replacedTurnId);
      if (!ack.running) return;
    } catch (error) {
      if (!options.current()) return;
      options.error(error);
    }
    while (options.current()) {
      if (now() - started >= 5000) warn();
      await delay();
      if (!options.current()) return;
      try {
        const status = await bounded(options.status());
        if (!options.current()) return;
        if (target && status.turnId && status.turnId !== target) return;
        options.update(status);
        if (!status.running) return;
      } catch (error) {
        if (!warned && options.current()) { warned = true; options.slow(); options.error(error); }
      }
    }
  } finally {
    clearTimeout(slowTimer);
  }
}
