package task

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/service"
	"go.uber.org/zap"
)

// MAX_PROCESSING_TICK_TIME is the maximum time a full round of processing may take.
const MAX_PROCESSING_TICK_TIME = 10 * time.Minute
const TASK_INTERVAL = 10 * time.Second
const SYSTEM_TASK_INTERVAL = 1 * time.Hour

// Daemon is for running tasks in the background
// ex: sending emails etc..
type Runner struct {
	CampaignService *service.Campaign
	UpdateService   *service.Update
	IsRunning       bool
	Logger          *zap.SugaredLogger
}

// Run starts the rask runner
// TODO implement a abort signal so things can be handled gracefully
// func (d *daemon) Run(abortSignal chan struct{}) {
func (d *Runner) Run(
	ctx context.Context,
	session *model.Session,
) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			d.Logger.Errorw("task runner panicked", "error", r, "stack", string(stack))
			d.Logger.Info("Restarting inline runner daemon in 5 seconds")
			time.Sleep(5 * time.Second)
			// Restart in a new goroutine to avoid recursive stack growth
			go d.Run(ctx, session)
		}
	}()
	//
	d.Logger.Debug("task runner started")
	// on the start of the next minute create a event loop that runs every minute
	// this is to ensure that the daemon runs every minute
	//lastFinishedAt := time.Now()
	for {
		now := time.Now()

		select {
		case <-ctx.Done():
			d.Logger.Debugf("Task runner stopping due to signal")
			return
		default:
			/*
				if lastFinishedAt.Add(time.Minute).After(now) {
					d.Logger.Warn("Last task took longer than a minute (processing tick) to complete")
				}
				d.Logger.Debugw("Task processing tick took", "error ,time.Since(now).Milliseconds())
			*/
			// sleep until the next minute change (ex 12:00:00 -> 12:01:00 and not 12:00:31 -> 12:01:31)
			//
			nextTick := now.Truncate(TASK_INTERVAL).Add(TASK_INTERVAL)

			time.Sleep(time.Until(nextTick))
			// time.Sleep(time.Until(now.Truncate(time.Minute).Add(time.Minute)))
			d.Process(
				ctx,
				session,
			)
		}
	}
}

func (d *Runner) RunSystemTasks(
	ctx context.Context,
	session *model.Session,
	wg *sync.WaitGroup,
) {
	// catch panics
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			d.Logger.Errorw("task runner panicked", "error", r, "stack", string(stack))
			d.Logger.Info("Restarting inline system runner daemon in 5 seconds")
			time.Sleep(5 * time.Second)
			// Restart in a new goroutine to avoid recursive stack growth
			go d.RunSystemTasks(ctx, session, nil) // Pass nil for wg to avoid double Done()
		}
	}()
	initialRunCompleted := false
	d.Logger.Debug("system task runner started")
	for {
		now := time.Now()
		// first task is done immediately
		d.ProcessSystemTasks(
			ctx,
			session,
		)
		if !initialRunCompleted {
			initialRunCompleted = true
			wg.Done()
		}

		select {
		case <-ctx.Done():
			d.Logger.Debugf("System Task runner stopping due to signal")
			return
		default:
			// time is not truncated to on the start of the next hour to avoid
			// all servers calling back at the same moment
			time.Sleep(time.Until(now.Add(SYSTEM_TASK_INTERVAL)))
		}
	}
}

// runTask runs a task
func (d *Runner) runTask(
	name string,
	fn func() error,
) {
	d.Logger.Debugw("task runner started", "name", name)
	now := time.Now()
	err := errs.Wrap(fn())
	if err != nil {
		d.Logger.Errorw("task runner failed", "name", name, "error", err)
	}
	d.Logger.Debugw(
		"task runner completed",
		"name", name,
		"duration", time.Since(now),
	)
}

// Process processes the tasks
func (d *Runner) Process(
	ctx context.Context,
	session *model.Session,
) {
	ctx, cancel := context.WithTimeoutCause(
		ctx,
		MAX_PROCESSING_TICK_TIME,
		fmt.Errorf("Processing tasks took over %f minutes", MAX_PROCESSING_TICK_TIME.Minutes()),
	)
	defer cancel()
	// update campaigns that are closed
	d.runTask("close campaigns", func() error {
		return d.CampaignService.HandleCloseCampaigns(
			ctx,
			session,
		)
	})
	// anonymize campaigns that are ready to be anonymized
	d.runTask("anonymize campaigns", func() error {
		return d.CampaignService.HandleAnonymizeCampaigns(
			ctx,
			session,
		)
	})
	d.Logger.Debug("task runner started processing")
	// send the next batch of messagess
	d.runTask("send messages", func() error {
		err := d.CampaignService.SendNextBatch(
			ctx,
			session,
		)
		return errs.Wrap(err)
	})
	d.Logger.Debug("task runner ended processing")
}

// Process system tasks
func (d *Runner) ProcessSystemTasks(
	ctx context.Context,
	session *model.Session,
) {
	ctx, cancel := context.WithTimeoutCause(
		ctx,
		MAX_PROCESSING_TICK_TIME,
		errs.Wrap(
			fmt.Errorf(
				"Processing tasks took over %f minutes", MAX_PROCESSING_TICK_TIME.Minutes(),
			),
		),
	)
	defer cancel()
	// check for updates
	d.runTask("system - check updates", func() error {
		if d.UpdateService == nil {
			d.Logger.Warn("UpdateService is nil, skipping update check")
			return nil
		}
		_, _, err := d.UpdateService.CheckForUpdate(ctx, session)
		return err
	})
}
