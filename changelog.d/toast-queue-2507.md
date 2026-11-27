### Changed

- `base/toast.Manager` queues toasts behind its `Limit`, highest priority then oldest first, merges a toast of one id into its place, and starts a waiting toast's timeout when it shows (#2507 by @im-apratap).
- `ui.Toaster` shows errors and toasts that stay until dismissed first, counts those waiting, and pauses its timeouts while the app is in the background (#2507 by @im-apratap).
- `ui.Toasts` reads its toasts as a polite status region with `Polite`, and takes their entering and leaving from `Motion`, none under `ReduceMotion` (#2507 by @im-apratap).
