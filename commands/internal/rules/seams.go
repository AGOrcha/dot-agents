package rules

import (
	"os"

	"github.com/AGOrcha/dot-agents/internal/platform"
)

// IO and downstream-library seams. Tests in this package swap these to
// error-injecting stubs to cover the error-return branches that cannot be
// triggered via filesystem fixtures (a writable tmp dir always reads back
// cleanly, etc.). Production code never overrides these.
var (
	osReadFile                       = os.ReadFile
	platformListCanonicalRuleFiles   = platform.ListCanonicalRuleFiles
	platformResolveCanonicalRuleFile = platform.ResolveCanonicalRuleFile
)
