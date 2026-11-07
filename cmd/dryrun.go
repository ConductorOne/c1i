package cmd

import (
	"github.com/spf13/viper"
)

// dryRunActive reports whether --dry-run / C1I_DRY_RUN is set. The mutation
// helper (cmd/mutation.go) previews instead of sending when it is.
func dryRunActive() bool {
	return viper.GetBool("dry_run")
}
