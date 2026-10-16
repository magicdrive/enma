package subcmd

import (
	"github.com/magicdrive/enma/internal/commandline/option"
	"github.com/magicdrive/enma/internal/core"
)

func Scheduler(args []string) error {
	opt, err := option.ParseScheduler(args)
	if err != nil {
		return err
	}
	return RunScheduler(opt)
}

// RunScheduler runs the scheduler. Unlike hotload / watch it does not change the
// process's directory: each job runs in its own working directory.
func RunScheduler(opt *option.SchedulerOption) error {
	return core.NewSchedulerRunner(opt).Start()
}
