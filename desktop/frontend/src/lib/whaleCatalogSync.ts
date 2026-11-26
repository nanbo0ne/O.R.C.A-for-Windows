import { app } from "./bridge";

let pending: Promise<void> | undefined;
export function syncWhaleCatalog() {
  return pending ??= app.WhaleStackRefreshModels()
    .then(() => { window.dispatchEvent(new Event("orca:whalestack-models")); })
    .finally(() => { pending = undefined; });
}
