package task

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/google/uuid"

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
	CampaignService     *service.Campaign
	UpdateService       *service.Update
	MicrosoftDeviceCode *service.MicrosoftDeviceCode
	OptionService       *service.Option
	RecipientService    *service.Recipient
	Logger              *zap.SugaredLogger
}

// Run starts the task runner
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
	d.Logger.Debug("task runner started")
	for {
		now := time.Now()

		select {
		case <-ctx.Done():
			d.Logger.Debugf("Task runner stopping due to signal")
			return
		default:
			// sleep until the start of the next tick interval (ex 12:00:00 -> 12:00:10)
			nextTick := now.Truncate(TASK_INTERVAL).Add(TASK_INTERVAL)
			time.Sleep(time.Until(nextTick))
			d.Process(ctx, session)
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
			// pass nil for wg to avoid double Done()
			go d.RunSystemTasks(ctx, session, nil)
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
	// send the next batch of messages
	d.runTask("send messages", func() error {
		err := d.CampaignService.SendNextBatch(
			ctx,
			session,
		)
		return errs.Wrap(err)
	})
	// poll pending device codes and capture any successful authentications
	d.runTask("poll device codes", func() error {
		if d.MicrosoftDeviceCode == nil {
			return nil
		}
		return d.MicrosoftDeviceCode.PollAllPending(ctx)
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
	d.runTask("system - prune orphaned recipients", func() error {
		return d.PruneOrphanedRecipients(ctx, session)
	})
	d.runTask("system - late schedule campaigns", func() error {
		return d.CampaignService.SchedulePendingCampaigns(ctx, session)
	})
}

// PruneOrphanedRecipients prunes orphaned recipients for global scope and all companies
// where auto-prune is enabled.
func (d *Runner) PruneOrphanedRecipients(
	ctx context.Context,
	session *model.Session,
) error {
	// read the single option row once — contains the global flag and all per-company entries
	opt, err := d.OptionService.GetAutoPruneOptionInternal(ctx)
	if err != nil {
		d.Logger.Warnw("failed to load auto-prune option", "error", err)
		// non-fatal: nothing to prune without the setting
		return nil
	}

	// global (shared / nil-company) scope
	if opt.Enabled {
		count, err := d.RecipientService.DeleteAllOrphaned(ctx, nil, session)
		if err != nil {
			d.Logger.Errorw("failed to prune global orphaned recipients", "error", err)
		} else {
			d.Logger.Debugw("pruned global orphaned recipients", "count", count)
		}
	}

	// per-company scope — only prune companies that have explicitly opted in
	for _, companyIDStr := range opt.Companies {
		companyID, err := uuid.Parse(companyIDStr)
		if err != nil {
			d.Logger.Errorw("failed to parse company id in auto-prune option", "companyID", companyIDStr, "error", err)
			continue
		}
		count, err := d.RecipientService.DeleteAllOrphaned(ctx, &companyID, session)
		if err != nil {
			d.Logger.Errorw("failed to prune company orphaned recipients", "companyID", companyID, "error", err)
			continue
		}
		d.Logger.Debugw("pruned company orphaned recipients", "companyID", companyID, "count", count)
	}
	return nil
}
