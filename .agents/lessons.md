# Lessons Index

- `tests-for-each-slice`: Each implementation slice should add automated coverage for the behavior it introduces before the slice is considered complete.
- `additive-state-fields`: When extending a central state struct, add a new field rather than reusing an existing one — keeps JSON contract stable and callers unbroken.
