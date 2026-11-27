package commands

import (
	"flag"
	"fmt"
	"time"

	"github.com/Kayungko/unity-pipe/internal/options"
	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// JobCommand inspects a detached job: upipe job <id> (polls until terminal by
// default) — mirrors GET /api/job.
func JobCommand() SubCommand {
	return SubCommand{
		Name: "job",
		Synopsis: "inspect a detached job: upipe job <id> [--no-wait | --stdout] [--log-after N] " +
			"(long builds/bakes: raise the wait budget with --timeout, e.g. --timeout 45m; default 10m)",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe job")
			noWait := fs.Bool("no-wait", false, "single snapshot instead of waiting for terminal state")
			stdout := fs.Bool("stdout", false, "write raw binary command output to stdout on success")
			logAfter := fs.Int64("log-after", 0, "resume job logs after this sequence (shown by --log-format json)")
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

			if *stdout && (rt.Opts.JSONOut || *noWait) {
				return nil, usageErr("job: --stdout cannot be combined with --json or --no-wait")
			}
			if *logAfter < 0 {
				return nil, usageErr("job: --log-after must be non-negative")
			}
			var resume bool
			fs.Visit(func(f *flag.Flag) {
				if f.Name == "log-after" {
					resume = true
				}
			})

			job := rest[0]
			cursor := *logAfter
			fetch := func(timeout time.Duration) (*protocol.JobResponse, error) {
				return rt.Client.GetJobWithLogs(job, cursor, timeout)
			}
			consume := func(reply *protocol.JobResponse) error {
				next, err := consumeJobLogs(reply, cursor, resume, logs)
				if err != nil {
					return err
				}
				cursor = next
				reply.Logs, reply.LogPage = nil, nil
				return nil
			}
			if *noWait {
				reply, err := fetch(rt.Opts.Timeout)
				if err != nil {
					return nil, err
				}
				if err := consume(reply); err != nil {
					return nil, err
				}
				return reply, nil
			}
			getJob := func() (*protocol.JobResponse, error) { return fetch(0) }
			var sleep protocol.SleepFunc
			if *stdout {
				budget := rt.Opts.Timeout
				if budget <= 0 {
					budget = options.DefaultTimeout
				}
				deadline := time.Now().Add(budget)
				getJob = func() (*protocol.JobResponse, error) {
					remaining := time.Until(deadline)
					if remaining <= 0 {
						return nil, protocol.ErrTimeout("deadline exceeded waiting for binary output")
					}
					reply, err := fetch(remaining)
					if time.Until(deadline) <= 0 {
						return nil, protocol.ErrTimeout("deadline exceeded waiting for binary output")
					}
					return reply, err
				}
				sleep = func(interval time.Duration) {
					time.Sleep(min(interval, time.Until(deadline)))
				}
			}
			result, err := protocol.Poll(func() (protocol.PollStep, error) {
				jr, err := getJob()
				if err != nil {
					return protocol.PollStep{}, err
				}
				if err := consume(jr); err != nil {
					return protocol.PollStep{}, err
				}
				if jr.State == "failed" && jr.ErrorCode == protocol.ReportedCommandFailure {
					return protocol.PollStep{}, protocol.ErrCommandFailed(&protocol.ExecResponse{Error: jr.Error, Result: jr.Result})
				}
				if jr.State == "completed" && jr.Result != nil {
					response := protocol.ExecResponse{Success: true, Result: jr.Result}
					if err := response.CommandError(jr.Command); err != nil {
						return protocol.PollStep{}, err
					}
				}
				if *stdout && jr.State == "completed" {
					return protocol.PollStep{Status: jr.State, Body: jr.Result}, nil
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
			}, sleep, nil)
			if err != nil {
				return nil, err
			}
			if *stdout {
				return decodeBinaryOutput(result.Body)
			}
			return jsonRaw(result.Body), nil
		},
	}
}

func consumeJobLogs(reply *protocol.JobResponse, after int64, resume bool, logs *commandLogFlags) (int64, error) {
	if reply.ErrorCode == protocol.JobUnavailable {
		return after, &protocol.UpipeError{
			Code:    protocol.ExitProtocol,
			Kind:    "JobUnavailable",
			Message: reply.Error,
			Data:    reply,
		}
	}
	page := reply.LogPage
	if page == nil {
		if resume || after > 0 {
			return after, protocol.ErrProtocol("this Pipeline instance did not return a resumable job log")
		}
		logs.write(reply.Logs)
		return after, nil
	}
	if page.NextCursor < after {
		return after, usageErr("job: --log-after is ahead of the job's last log sequence")
	}
	if page.Dropped < 0 || page.Dropped > page.NextCursor-after ||
		int64(len(page.Entries)) != page.NextCursor-after-page.Dropped {
		return after, protocol.ErrProtocol("invalid job log page")
	}
	for index, entry := range page.Entries {
		if entry.Sequence != after+page.Dropped+int64(index)+1 {
			return after, protocol.ErrProtocol("job log page is out of sequence")
		}
	}
	if page.Dropped > 0 {
		logs.write([]protocol.CommandLog{{
			Level:   "warning",
			Message: fmt.Sprintf("job log retention skipped %d entries", page.Dropped),
			Dropped: page.Dropped,
		}})
	}
	logs.write(page.Entries)
	return page.NextCursor, nil
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
