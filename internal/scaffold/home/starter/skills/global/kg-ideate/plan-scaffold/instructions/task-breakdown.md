# Task Breakdown — Phase 3 Step 11

Rules for Phase 3 task breakdown + dependency ordering:

- Turn spec requirements into concrete, bounded tasks.
- Each task has a single, observable outcome (one thing verifiable, not "implement X and Y").
- Order with explicit `depends_on` relationships; never leave ordering implicit.
- Cross-plan deps use `<plan-id>/<task-id>` form. A dep string is cross-plan iff it
  contains `/`. Example: `depends_on: [kg-ideate-skill/p0-compound-skill]`.
- A task with a dependency on an external plan that has not been started yet should be
  flagged in its notes with the blocking plan ID.
