package events

// Cross-plan event payload shapes.
//
// Payload-ownership note (spec D4.0): the transport seam (EventBus) treats
// every payload as opaque `any` and never inspects it — but the payload
// SHAPES that ride the R3 topics are cross-plan contracts (r2's topic bridge
// and SSE fan-out, r5's collection endpoint type-assert them), so they live
// here in the contract package alongside the topic constants, not in the
// producing task's package. Producers construct them; the bus stays
// shape-blind.

// IterationScored is the payload published on TopicIterationScored after an
// iteration log entry has been scored and its score sidecar written.
// Subscribers treat it as a wake-up: the sidecar at SidecarPath is the
// canonical state (guarantee G1 — a dropped event is recovered by re-reading
// disk, never from the bus).
type IterationScored struct {
	Iteration   int     `json:"iter"`
	Score       float64 `json:"score"`
	Band        string  `json:"band"`
	SidecarPath string  `json:"sidecar_path"`
}

// RescoreDone is the payload published on TopicRescoreDone after a rubric
// version bump has driven a full-log rescore and the refreshed per-iteration
// and per-session sidecars are on disk. Subscribers treat it as a wake-up:
// the sidecars (and the rescore watermark, persisted before this publish)
// are the canonical state per guarantee G1.
type RescoreDone struct {
	FromVersion string `json:"from_version"`
	ToVersion   string `json:"to_version"`
	IterCount   int    `json:"iter_count"`
}
