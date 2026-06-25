package complianceregister_test

// Shared test literals hoisted so no single _test.go file repeats a string
// often enough to trip SonarCloud S1192 (the coverage gate's new-issues check
// blocks on duplicated literals in tests too).
const (
	tControl       = "control"
	tPolicy        = "policy"
	tFinding       = "finding"
	tEvidence      = "evidence"
	fStatus        = "status"
	fStale         = "stale"
	fReason        = "reason"
	fDerives       = "derives_from"
	fFramework     = "framework"
	fExpiresAt     = "expires_at"
	vEffective     = "effective"
	vEnvironmental = "environmental"
	vDerivation    = "derivation"
	idCtl1         = "ctl-1"
	idCtl2         = "ctl-2"
	idPol1         = "pol-1"
	frameworkSOC2  = "SOC2"
	idCtlMFA       = "AC-2-MFA"
)
