# Old orchestrator DMA artifact reorganization

- [x] Inventory existing cleanup edits and preserve unrelated work.
- [x] Resolve stale delegation ownership from `parent_plan_id` and `parent_task_id`.
- [x] Move delegation bundles, delegation records, merge-backs, verification, and review artifacts into `.agents/history/<plan-id>/delegate-merge-back-archive/<date>/<task-id>/`.
- [x] Remove duplicate salvage-bucket copies after confirming canonical destinations.
- [x] Audit remaining `.agents/active` DMA directories and validate history structure.
- [x] Archive this plan with an implementation result.
