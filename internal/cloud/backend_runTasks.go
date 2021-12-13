package cloud

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/go-tfe"
)

type taskResultSummary struct {
	pending         int
	failed          int
	failedMandatory int
	passed          int
}

type taskStageReadFunc func(b *Cloud, stopCtx context.Context) (*tfe.TaskStage, error)

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

// elapsedMessageMax is 50 chars: the length of this message with 6 digits
// 99 tasks still pending, 99 passed, 99 failed ...
const elapsedMessageMax int = 50

func (b *Cloud) runTasksWithTaskResults(subtask *Subtask, fetchTaskStage taskStageReadFunc) error {
	started := time.Now()
	for i := 0; ; i++ {
		select {
		case <-subtask.StopContext.Done():
			return subtask.StopContext.Err()
		case <-subtask.CancelContext.Done():
			return subtask.CancelContext.Err()
		case <-time.After(backoff(backoffMin, backoffMax, i)):
			// waits time to elapse, then recheck tasks statuses
		}
		// checking if i == 0 so as to avoid printing this starting horizontal-rule
		// every retry, and that it only prints it on the first (i=0) attempt.
		if i == 0 {
			subtask.OutputBegin()
		}

		// TODO: get the stage that corresponds to an argument passed to this function
		stage, err := fetchTaskStage(b, subtask.StopContext)

		if err != nil {
			return generalError("Failed to retrieve pre-apply task stage", err)
		}

		summary := summarizeTaskResults(stage.TaskResults)
		if summary.pending > 0 {
			message := fmt.Sprintf("%d tasks still pending, %d passed, %d failed ... ", summary.pending, summary.passed, summary.failed)

			if i%4 == 0 {
				if i > 0 {
					subtask.OutputPendingElapsed(started, message, elapsedMessageMax)
				}
			}
			continue
		}

		// No more tasks pending/running. Print all the results.

		// Track the first task name that is a mandatory enforcement level breach.
		var firstMandatoryTaskFailed *string = nil

		if i == 0 {
			subtask.Output(fmt.Sprintf("All tasks completed! %d passed, %d failed", summary.passed, summary.failed))
		} else {
			subtask.OutputPendingElapsed(started, fmt.Sprintf("All tasks completed! %d passed, %d failed", summary.passed, summary.failed), 50)
		}
		subtask.Output("")

		for _, t := range stage.TaskResults {
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

			title := fmt.Sprintf(`%s ⸺   %s`, t.TaskName, status)
			subtask.OutputColor(title)

			subtask.OutputColor(fmt.Sprintf("[dim]%s", t.Message))
			subtask.OutputColor("")
		}

		// If a mandatory enforcement level is breached, return an error.
		var taskErr error = nil
		var overall string = "[green]Passed"
		if firstMandatoryTaskFailed != nil {
			overall = "[red]Failed"
			taskErr = fmt.Errorf("the run failed because the run task, %s, is required to succeed", *firstMandatoryTaskFailed)
		}

		if summary.failed-summary.failedMandatory > 0 {
			overall = "[green]Passed with advisory failures"
		}

		subtask.OutputColor("")
		subtask.OutputColor("[bold]Overall Result: " + overall)

		subtask.OutputEnd()

		return taskErr
	}
}

func (b *Cloud) runTasks(subtask *Subtask) error {
	return b.runTasksWithTaskResults(subtask, func(b *Cloud, stopCtx context.Context) (*tfe.TaskStage, error) {
		options := tfe.TaskStageReadOptions{
			Include: "task_results",
		}

		return b.client.TaskStages.Read(subtask.StopContext, subtask.Run.TaskStage[0].ID, &options)
	})
}
