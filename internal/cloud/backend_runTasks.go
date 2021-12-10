package cloud

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform/internal/backend"
)

type taskResultSummary struct {
	pending         int
	failed          int
	failedMandatory int
	passed          int
}

func getPreApplyTaskStage(b *Cloud, stopCtx context.Context, taskStageId string) (*tfe.TaskStage, error) {
	options := tfe.TaskStageReadOptions{
		Include: "task_results",
	}

	return b.client.TaskStages.Read(stopCtx, taskStageId, &options)
}

func summarizeTaskResults(taskResults []*tfe.TaskResult) taskResultSummary {
	var pe, er, erm, pa int
	for _, task := range taskResults {
		if task.Status == "running" || task.Status == "pending" {
			pe++
		} else if task.Status == "passed" {
			pa++
		} else {
			// Everything else is a failure
			er++
			if task.WorkspaceTaskEnforcementLevel == "mandatory" {
				erm++
			}
		}
	}

	return taskResultSummary{
		pending:         pe,
		failed:          er,
		failedMandatory: erm,
		passed:          pa,
	}
}

func (b *Cloud) runTasks(stopCtx context.Context, cancelCtx context.Context, op *backend.Operation, r *tfe.Run) error {
	msgPrefix := "Run tasks"
	started := time.Now()

	for i := 0; ; i++ {
		select {
		case <-stopCtx.Done():
			return stopCtx.Err()
		case <-cancelCtx.Done():
			return cancelCtx.Err()
		case <-time.After(backoff(backoffMin, backoffMax, i)):
			// waits time to elapse, then recheck tasks statuses
		}
		// checking if i == 0 so as to avoid printing this starting horizontal-rule
		// every retry, and that it only prints it on the first (i=0) attempt.
		if b.CLI != nil && i == 0 {
			b.CLI.Output("\n------------------------------------------------------------------------\n")
			b.CLI.Output(b.Colorize().Color(msgPrefix + ":\n"))
		}

		taskStage, err := getPreApplyTaskStage(b, stopCtx, r.TaskStage[0].ID)

		if err != nil {
			return generalError("Failed to retrieve pre-apply task stage", err)
		}

		summary := summarizeTaskResults(taskStage.TaskResults)

		current := time.Now()
		elapsed := current.Sub(started).Truncate(1 * time.Second)
		elapsedMsg := ""
		if summary.pending > 0 {
			// Example pending output; the variable spacing allows up to 99 tasks (two digits) in each category:
			// ---------------
			// 3 tasks still pending, 0 passed, 0 failed ...
			// 3 tasks still pending, 0 passed, 0 failed ...       (8s elapsed)
			// 3 tasks still pending, 0 passed, 0 failed ...       (19s elapsed)
			// 3 tasks still pending, 0 passed, 0 failed ...       (33s elapsed)

			message := fmt.Sprintf("%d tasks still pending, %d passed, %d failed ... ", summary.pending, summary.passed, summary.failed)
			allocatedChars := len(" tasks still pending,  passed,  failed ... ") + (3 * 2) // 3 placeholders, up to 2 digits each
			spacing := strings.Repeat(" ", allocatedChars-len(message))                    // aligns the elapsed field to the right

			if b.CLI != nil && i%4 == 0 {
				if i > 0 {
					elapsedMsg = b.Colorize().Color(fmt.Sprintf("%s[dark_gray](%s elapsed)", spacing, elapsed))
				}
				b.CLI.Output(message + elapsedMsg)
			}
			continue
		}

		// No more tasks pending/running. Print all the results.

		// Track the first task name that is a mandatory enforcement level breach.
		var firstMandatoryTaskFailed *string = nil
		if b.CLI != nil {
			b.CLI.Output(fmt.Sprintf("All tasks completed! %d passed, %d failed\n", summary.passed, summary.failed))
		}

		for _, t := range taskStage.TaskResults {
			capitalizedStatus := string(t.Status)
			capitalizedStatus = strings.ToUpper(capitalizedStatus[:1]) + capitalizedStatus[1:]

			status := "[green]" + capitalizedStatus
			if t.Status != "passed" {
				level := string(t.WorkspaceTaskEnforcementLevel)
				level = strings.ToUpper(level[:1]) + level[1:]
				status = fmt.Sprintf("[red]%s (%s)", capitalizedStatus, level)

				if t.WorkspaceTaskEnforcementLevel == "mandatory" && firstMandatoryTaskFailed == nil {
					firstMandatoryTaskFailed = &t.TaskName
				}
			}
			if b.CLI != nil {
				title := b.Colorize().Color(fmt.Sprintf(`[reset]│ %s ⸺   %s[reset]`, t.TaskName, status))
				b.CLI.Output(b.Colorize().Color(title))

				message := strings.ReplaceAll(t.Message, "\n", "\n[reset]│ [dark_gray]")
				b.CLI.Output(b.Colorize().Color(fmt.Sprintf("[reset]│ [dark_gray]%s[reset]", message)))
			}
		}

		// If a mandatory enforcement level is breached, return an error.
		var taskErr error = nil
		if firstMandatoryTaskFailed != nil {
			taskErr = fmt.Errorf("the run failed because the run task, %s, is required to succeed", *firstMandatoryTaskFailed)
		}

		if b.CLI != nil {
			b.CLI.Output("\n------------------------------------------------------------------------\n")
		}

		return taskErr
	}
}
