---
name: provider-consumer-pair
description: "Use when two waves or phases must be implemented together because one defines a contract and the other consumes it, and you need to sequence the work without circular blocking."
---

# Provider Consumer Pair

Coordinate paired provider and consumer implementation work across related plan waves.

## Workflow

1. **Load the paired implementation workflow**
   Load → `instructions/workflow.md`
   Read both plans, identify the contract boundary, and sequence provider and consumer work.

2. **Review failure points**
   Load → `instructions/gotchas.md`
   Check the cross-layer and integration pitfalls before splitting the work.
