// Package exitcode lists process statuses from the CLI contract.
package exitcode

const (
	Success        = 0
	Generic        = 1
	Invalid        = 2
	Discovery      = 3
	Upgrade        = 4
	Validation     = 5
	NeedsReview    = 6
	Policy         = 7
	ToolMissing    = 8
	DirtyWorkspace = 9
	Interrupted    = 10
)
