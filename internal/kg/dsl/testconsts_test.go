package dsl_test

// Shared test literals, hoisted so no single test file repeats a string literal
// often enough to trip SonarCloud S1192 (the coverage gate's new-issues check
// blocks on duplicated literals in _test.go too). These are the domain note
// types, field names, ids, and enum values the conformance and eval tests reuse.
const (
	// note types
	tCharacter = "character"
	tLocation  = "location"
	tFunction  = "Function"
	tControl   = "control"
	tPolicy    = "policy"
	tFinding   = "finding"
	tEvidence  = "evidence"

	// field types / field names
	tString         = "string"
	fRegion         = "region"
	fStatus         = "status"
	fStatedLocation = "stated_location"
	fExpiresAt      = "expires_at"
	fDerivesFrom    = "derives_from"
	fStale          = "stale"
	fReason         = "reason"

	// enum / value literals
	vAlive         = "alive"
	vEffective     = "effective"
	vEnvironmental = "environmental"

	// ids
	idCharA = "char-a"
	idLocEU = "loc-eu"
	idLocUS = "loc-us"
	idPol1  = "pol-1"
	idCtl1  = "ctl-1"
	idCtl2  = "ctl-2"
)

// Additional column/field literals used in single files but repeated enough to
// trip S1192.
const (
	colStaleReason = "c.stale.reason"
	fActive        = "active"
)
