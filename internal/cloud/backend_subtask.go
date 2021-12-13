package cloud

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform/internal/backend"
)


type Subtask struct {
	Name          string
	B             *Cloud
	StopContext   context.Context
	CancelContext context.Context
	Op            *backend.Operation
	Run           *tfe.Run
}

func (s *Subtask) hasCLI() bool {
	return s.B.CLI != nil
}

func (s *Subtask) OutputBegin() {
	if !s.hasCLI() {
		return
	}

	s.B.CLI.Output("\n------------------------------------------------------------------------\n")
	s.B.CLI.Output(s.B.Colorize().Color("[bold]" + s.Name + ":\n"))
}

func (s *Subtask) OutputEnd() {
	if !s.hasCLI() {
		return
	}

	s.B.CLI.Output("\n------------------------------------------------------------------------\n")
}

func (s *Subtask) OutputColor(str string) {
	s.Output(s.B.Colorize().Color(str))
}

func (s *Subtask) Output(str string) {
	if !s.hasCLI() {
		return
	}
	s.B.CLI.Output(s.B.Colorize().Color("[reset]│ ") + str)
}

// Example pending output; the variable spacing (50 chars) allows up to 99 tasks (two digits) in each category:
// ---------------
// 13 tasks still pending, 0 passed, 0 failed ...
// 13 tasks still pending, 0 passed, 0 failed ...       (8s elapsed)
// 13 tasks still pending, 0 passed, 0 failed ...       (19s elapsed)
// 13 tasks still pending, 0 passed, 0 failed ...       (33s elapsed)
func (s *Subtask) OutputPendingElapsed(since time.Time, message string, maxMessage int) {
	if !s.hasCLI() {
		return
	}
	elapsed := time.Since(since).Truncate(1 * time.Second)
	s.B.CLI.Output(fmt.Sprintf("%-"+strconv.FormatInt(int64(maxMessage), 10)+"s", message) + s.B.Colorize().Color(fmt.Sprintf("[dim](%s elapsed)", elapsed)))
}
