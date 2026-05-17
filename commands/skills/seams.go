package skills

import (
	"os"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

// IO and downstream-library seams. Tests in this package swap these to
// error-injecting stubs to cover the error-return branches that cannot be
// triggered via filesystem fixtures (MkdirAll on a writable tmp dir always
// succeeds, etc.). Production code never overrides these.
var (
	osMkdirAll  = os.MkdirAll
	osWriteFile = os.WriteFile
	osSymlink   = os.Symlink
	configLoad  = config.Load
)
