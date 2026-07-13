package sddregister

// NewFromYAML exposes the fallible constructor to external tests so the
// invalid-schema and bad-query error paths (which New panics on, and which
// cannot occur for the shipped embed) are exercised directly.
func NewFromYAML(yaml []byte) (*Adapter, error) {
	return newFromYAML(yaml)
}
