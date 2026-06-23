# Commit and Clean Up

How to commit work and clean up the codebase before handoff.

## Stage and Commit Finished Work

- Commit all completed changes with clear, descriptive commit messages
- Group related changes into logical commits — do not lump everything together
- If work is partially complete, commit what is stable and note the rest in handoff notes

## Remove Debugging Code

- Search for and remove `console.log`, `debugger`, `print()`, or equivalent debugging statements you added
- Remove any temporary test data or hardcoded values used during development
- Revert any config changes made purely for local debugging

## Delete Temporary Files

- Remove scratch files, test outputs, or temporary scripts created during the session
- Check for any files that should be in `.gitignore` but are not

## Ensure Tests Pass

- Run the full test suite (or at minimum, tests related to your changes)
- If tests fail due to your changes, fix them before handoff
- If tests fail for unrelated reasons, document this clearly in handoff notes — do not leave the next session guessing
