package commands

import (
	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// JobCommand inspects a detached job: upipe job <id> (polls until terminal by
// default) — mirrors GET /api/job.
func JobCommand() SubCommand {
	return SubCommand{
		Name: "job",
		Synopsis: "inspect a detached job: upipe job <id> [--no-wait] " +
			"(long builds/bakes: raise the wait budget with --timeout, e.g. --timeout 45m; default 10m)",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe job")
			noWait := fs.Bool("no-wait", false, "single snapshot instead of waiting for terminal state")
			logs := attachCommandLogFlags(fs)
			attachShared(fs, rt.Opts)
			rest, err := parseArgs(fs, args)
			if err != nil {
				return nil, usageErr("job: " + err.Error())
			}
			if len(rest) != 1 {
				return nil, usageErr("job: exactly one job id expected")
			}
			if err := logs.validate(); err != nil {
				return nil, err
			}

			job := rest[0]
			if *noWait {
				reply, err := rt.Client.GetJob(job)
				if err != nil {
					return nil, err
				}
				logs.write(reply.Logs)
				reply.Logs = nil
				return reply, nil
			}
			result, err := protocol.Poll(func() (protocol.PollStep, error) {
				jr, err := rt.Client.GetJob(job)
				if err != nil {
					return protocol.PollStep{}, err
				}
				logs.write(jr.Logs)
				jr.Logs = nil
				if jr.State == "failed" && jr.ErrorCode == protocol.ReportedCommandFailure {
					return protocol.PollStep{}, protocol.ErrCommandFailed(&protocol.ExecResponse{Error: jr.Error, Result: jr.Result})
				}
				if jr.State == "completed" && jr.Result != nil {
					response := protocol.ExecResponse{Success: true, Result: jr.Result}
					if err := response.CommandError(jr.Command); err != nil {
						return protocol.PollStep{}, err
					}
				}
				return protocol.PollStep{Status: jr.State, Body: mustMarshal(jr)}, nil
			}, protocol.PollConfig{
				TerminalOK:     []string{"completed"},
				TerminalFail:   []string{"failed"},
				TerminalCancel: []string{"canceled", "cancelled"},
				// --timeout (a shared duration flag, e.g. 45m) extends the
				// poll budget for long jobs (builds, bakes); without it the
				// wait dies at the 10min default (options.DefaultTimeout).
				// The synopsis above surfaces this so long waits are not a
				// surprise cutoff.
				Deadline: rt.Opts.Timeout,
			}, nil, nil)
			if err != nil {
				return nil, err
			}
			return jsonRaw(result.Body), nil
		},
	}
}

// JobCancelCommand cancels a detached job (POST /api/job/cancel).
func JobCancelCommand() SubCommand {
	return SubCommand{
		Name:     "job-cancel",
		Aliases:  []string{"cancel"},
		Synopsis: "request cancellation of a detached job: upipe job-cancel <id>",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe job-cancel")
			attachShared(fs, rt.Opts)
			rest, err := parseArgs(fs, args)
			if err != nil {
				return nil, usageErr("job-cancel: " + err.Error())
			}
			if len(rest) != 1 {
				return nil, usageErr("job-cancel: exactly one job id expected")
			}
			return rt.Client.CancelJob(rest[0])
		},
	}
}

// ProgressCommand shows the server-side command progress
// ({"active": bool, "progress": …}).
func ProgressCommand() SubCommand {
	return SubCommand{
		Name:     "progress",
		Synopsis: "show progress of the currently executing command",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe progress")
			attachShared(fs, rt.Opts)
			if _, err := parseArgs(fs, args); err != nil {
				return nil, usageErr("progress: " + err.Error())
			}
			return rt.Client.Progress()
		},
	}
}
