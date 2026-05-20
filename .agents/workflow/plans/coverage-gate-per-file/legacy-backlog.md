# coverage-gate-per-file — cg5 legacy remediation backlog

Per-file offenders <95% (merged multi-OS profile, baseline from PR #24
green run 76450226338 / commit 6b9649ad). Each: test up to >=95% OR allowlist
with rationale in scripts/coverage-exceptions.txt. Phase 1 = warn;
cg6 ratchets to enforce once this list is cleared.

## Per-file (FAIL, not allowlisted)

commands/add.go                                                      93.84%  FAIL
commands/agents/promote.go                                           85.71%  FAIL
commands/doctor.go                                                   94.78%  FAIL
commands/hooks/cmd.go                                                92.86%  FAIL
commands/hooks/remove.go                                             89.74%  FAIL
commands/hooks/spec.go                                               93.75%  FAIL
commands/import_plugins.go                                           93.39%  FAIL
commands/import.go                                                   94.94%  FAIL
commands/init.go                                                     81.91%  FAIL
commands/linkcount_unix.go                                           71.43%  FAIL
commands/linkcount_windows.go                                        72.73%  FAIL
commands/refresh.go                                                  93.53%  FAIL
commands/sync.go                                                     85.71%  FAIL
commands/sync/pull.go                                                88.89%  FAIL
commands/workflow.go                                                 33.33%  FAIL
commands/workflow/app_types.go                                       94.12%  FAIL
commands/workflow/delegation.go                                      93.74%  FAIL
commands/workflow/drift.go                                           94.30%  FAIL
commands/workflow/fs.go                                              93.27%  FAIL
commands/workflow/graph.go                                           93.28%  FAIL
commands/workflow/health.go                                          94.67%  FAIL
commands/workflow/iter_log_schema.go                                 75.00%  FAIL
commands/workflow/iter_log.go                                        94.27%  FAIL
commands/workflow/review_decision_schema.go                          88.52%  FAIL
commands/workflow/review_gate.go                                     93.18%  FAIL
commands/workflow/verification_result_schema.go                      88.71%  FAIL
internal/config/config.go                                            93.18%  FAIL
internal/config/proposals.go                                         92.92%  FAIL
internal/fsops                                                        7.27%  FAIL
internal/fsops/fsops_windows.go                                       0.00%  FAIL
internal/graphstore/crg.go                                           94.69%  FAIL
internal/graphstore/postgres.go                                      91.56%  FAIL
internal/graphstore/sqlite.go                                        94.65%  FAIL
internal/links                                                       89.24%  FAIL
internal/links/inode_unix.go                                         88.89%  FAIL
internal/links/inode_windows.go                                      79.41%  FAIL
internal/links/symlink_windows.go                                    77.08%  FAIL
internal/platform/copilot.go                                         94.71%  FAIL
internal/platform/cursor.go                                          92.58%  FAIL
internal/platform/hooks.go                                           94.43%  FAIL
internal/platform/mcp_settings.go                                    94.74%  FAIL
internal/platform/opencode.go                                        91.78%  FAIL
internal/platform/plugins.go                                         93.41%  FAIL
internal/platform/render_manifest.go                                 93.94%  FAIL
internal/platform/resource_plan.go                                   94.47%  FAIL
internal/projectsync/promote.go                                      94.02%  FAIL

## Per-package (warn) — informational; cg6 enforces per-file, package gate retired
(warn: package(s) below 95%)
