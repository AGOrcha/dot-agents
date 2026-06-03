## Diagrams as Views, Not Maps: Diagram Discipline at Scale

**Source**: https://www.reddit.com/r/softwarearchitecture/comments/1tpamhi/how_do_you_keep_diagrams_useful_as_systems_grow/
**Author**: frovelli (r/softwarearchitecture)
**Date**: Unknown
**Method**: Manual copy
**Word count**: ~560 words

---

### Summary

The author argues that the hard part of architecture diagrams in large systems is not drawing many nodes but deciding which question a diagram is meant to answer. They advocate treating diagrams as purpose-specific views rather than complete maps, keeping each view focused on one concern, and never letting the drawing become the source of truth. Diagrams should be derived from artifacts already in the engineering lifecycle and updated alongside the code and interface changes that affect them, so they do not drift into "documentation theatre."

---

### Body

I don't think the real problem is how to draw 30+ nodes.

The real problem is deciding what question the diagram is supposed to answer.

In large systems, I try not to treat architecture diagrams as complete maps of the system. Complete maps usually become unreadable very quickly, or they become something people keep around because "we need a diagram", even if nobody really uses it.

What works better, in my experience, is to treat diagrams as views.

A view should exist because someone needs to reason about something specific. For example:

- who owns what
- what talks to what at runtime
- what is deployed where
- how data moves through the system
- where trust boundaries are
- how failures propagate
- what operators need to understand during an incident

The same component can appear in more than one view. That is fine. It is not duplication if the views answer different questions. The mistake I see quite often is trying to put too many concerns into the same picture.

A diagram that shows services, databases, teams, protocols, deployment zones, events, sync calls, async flows, security boundaries and business domains all at once usually stops being useful. At that point it is not really an architecture diagram anymore. It is just a place where every concern has been dumped.

For larger systems, I usually follow a few rules.

First, one diagram should answer one main question. If I cannot describe the diagram as "this helps us reason about X", then I probably should not create it.

Second, the drawing should not be the real source of truth. The drawing is only a projection. The source of truth may be code, service metadata, deployment manifests, interface contracts, ADRs, schemas, or some more formal architecture model. But if the diagram is just a manually maintained artifact on the side, it will drift. Not maybe. It will.

Third, interfaces matter more than boxes.

At scale, the important architectural knowledge is often not the list of nodes. It is the contract between them: data ownership, latency assumptions, retry behavior, versioning, failure semantics, observability, deployment coupling, and operational responsibility. Hierarchy can help, but I would not make hierarchy the main principle.

Some systems decompose cleanly. Many do not. Real flows often cut across hierarchy: shared data products, event streams, operational dependencies, incident paths, failure propagation, reporting chains, compliance boundaries.

So I would not try to solve this by creating a better "big diagram". I would rather create smaller views, each one with a clear purpose, preferably derived from a shared model or at least from artifacts that are already part of the engineering lifecycle.

C4 can help, but only if it is used as a thinking tool. If the discussion becomes mostly about which C4 level something belongs to, instead of what decision the diagram is supposed to support, then the team is probably focusing on the wrong thing.

For keeping diagrams current, I would attach them to the same lifecycle as code and interface changes. If a PR changes a service boundary, an API contract, an event schema, a deployment topology, or a failure assumption, then the relevant architectural view should be updated as part of that change. Otherwise the diagram slowly turns into documentation theatre.

So, my answer would be "don't try to make the big diagram readable". Make the architectural knowledge accessible through focused views.

---

### Key Quotes

> "The real problem is deciding what question the diagram is supposed to answer."

> "It is not duplication if the views answer different questions."

> "The drawing should not be the real source of truth. The drawing is only a projection."

> "Otherwise the diagram slowly turns into documentation theatre."

---

### Extraction Notes

This is a Reddit *comment* (not an article) left on the linked r/softwarearchitecture thread. It was copied manually because automated extraction hit Reddit's login wall. Because a comment carries no headline, the title above is descriptive and assigned, not verbatim. No publication date was captured in the manual copy, so the date is recorded as Unknown.
