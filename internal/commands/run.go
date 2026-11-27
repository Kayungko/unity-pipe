package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Kayungko/unity-pipe/internal/options"
	"github.com/Kayungko/unity-pipe/internal/protocol"
)

type batchRunResult struct {
	LogFile        string `json:"logFile"`
	EditorPID      int    `json:"editorPid,omitempty"`
	EditorExitCode *int   `json:"editorExitCode,omitempty"`
	Stage          string `json:"stage"`
	Result         any    `json:"result,omitempty"`
}

func RunCommand() SubCommand {
	return SubCommand{
		Name:     "run",
		Synopsis: "run exec, test or build in a temporary batchmode Editor: upipe run --unity PATH --project-path DIR -- SUBCOMMAND",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe run")
			unity := fs.String("unity", "", "path to the Unity Editor executable")
			logPath := fs.String("log-file", "", "new Editor log path (default: unique log under the project's Logs directory)")
			attachShared(fs, rt.Opts)
			separator := slices.Index(args, "--")
			flags := args
			if separator >= 0 {
				flags = args[:separator]
			}
			rest, err := parseArgs(fs, flags)
			if err != nil {
				return nil, usageErr("run: " + err.Error())
			}
			if len(rest) != 0 || separator < 0 || separator+1 == len(args) {
				return nil, usageErr("run: provide '-- exec COMMAND', '-- test [flags]' or '-- build [flags]' after the Editor options")
			}
			if *unity == "" || rt.Opts.ProjectPath == "" || rt.Opts.PlayerPath != "" {
				return nil, usageErr("run: --unity and --project-path are required")
			}
			if rt.Opts.Timeout < 0 {
				return nil, usageErr("run: --timeout must be positive")
			}
			nested := args[separator+1:]
			var command SubCommand
			switch nested[0] {
			case "exec":
				if len(nested) < 2 || strings.HasPrefix(nested[1], "-") {
					return nil, usageErr("run: provide the command name immediately after exec")
				}
				switch nested[1] {
				case "build", "run_tests", "wait_start", "editor_quit":
					return nil, usageErr("run: exec " + nested[1] + " does not own the operation's full lifecycle")
				}
				command = ExecCommand()
			case "test":
				command = TestCommand()
			case "build":
				command = BuildCommand()
			default:
				return nil, usageErr("run: only exec, test and build are supported")
			}
			for _, arg := range nested[1:] {
				name, _, _ := strings.Cut(strings.TrimLeft(arg, "-"), "=")
				if !strings.HasPrefix(arg, "-") {
					continue
				}
				switch name {
				case "project-path", "player-path", "timeout", "stdin", "stdout", "job", "no-wait", "plan":
					return nil, usageErr("run: --" + name + " is unavailable after the command separator")
				}
			}
			if _, err := os.Stat(filepath.Join(rt.ProjectPath, "ProjectSettings", "ProjectVersion.txt")); err != nil {
				return nil, usageErr("run: --project-path must contain a Unity project")
			}
			return runBatchEditor(rt, *unity, *logPath, command, nested[1:])
		},
	}
}

func reserveLog(project, requested string) (string, error) {
	var file *os.File
	var err error
	if requested == "" {
		logs := filepath.Join(project, "Logs")
		if err := os.MkdirAll(logs, 0755); err != nil {
			return "", err
		}
		file, err = os.CreateTemp(logs, "upipe-run-*.log")
	} else {
		abs, absErr := filepath.Abs(requested)
		if absErr != nil {
			return "", absErr
		}
		file, err = os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	}
	if err != nil {
		return "", err
	}
	return file.Name(), file.Close()
}

type batchEditor struct {
	process *os.Process
	done    chan struct{}
	state   *os.ProcessState
	close   func() error
}

func startBatchEditor(unity, project, logFile string) (*batchEditor, error) {
	executable, err := filepath.Abs(unity)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(executable, "-batchmode", "-nographics", "-projectPath", project, "-logFile", logFile)
	cmd.Dir = project
	closeScope, err := startScopedEditor(cmd)
	if err != nil {
		return nil, err
	}
	editor := &batchEditor{process: cmd.Process, done: make(chan struct{}), close: closeScope}
	go func() {
		_ = cmd.Wait()
		editor.state = cmd.ProcessState
		close(editor.done)
	}()
	return editor, nil
}

func (editor *batchEditor) exited() bool {
	select {
	case <-editor.done:
		return true
	default:
		return false
	}
}

func (editor *batchEditor) exitCode() *int {
	if !editor.exited() || editor.state == nil {
		return nil
	}
	code := editor.state.ExitCode()
	return &code
}

func (editor *batchEditor) ownedSource(source protocol.DescriptorSource) protocol.DescriptorSource {
	return func() (*protocol.InstanceDescriptor, error) {
		if editor.exited() {
			return nil, fmt.Errorf("the temporary Editor has exited")
		}
		descriptor, err := source()
		if err != nil {
			return nil, err
		}
		if descriptor.Pid != editor.process.Pid || descriptor.Mode != "batchmode" || descriptor.EvalToken == "" {
			return nil, fmt.Errorf("waiting for the temporary Editor's own batchmode descriptor")
		}
		return descriptor, nil
	}
}

func (editor *batchEditor) waitReady(ctx context.Context, client *protocol.PipelineClient) error {
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		raw, err := client.StatusWithin(time.Second)
		if ctx.Err() != nil {
			return batchContextError(ctx.Err(), "startup")
		}
		if err == nil {
			var status struct {
				Status string `json:"status"`
				Phase  string `json:"phase"`
			}
			if json.Unmarshal(raw, &status) == nil {
				switch status.Phase {
				case "ready":
					return nil
				case "compile_failed", "error", "blocked_by_dialog":
					return batchErr("EditorStartupFailed", "temporary Editor entered "+status.Phase)
				case "":
					if status.Status == "ready" {
						return nil
					}
				}
			}
		}
		select {
		case <-editor.done:
			return batchErr("EditorStartupFailed", "temporary Editor exited before Pipeline became ready")
		case <-ctx.Done():
			return batchContextError(ctx.Err(), "startup")
		case <-tick.C:
		}
	}
}

func batchErr(kind, message string) *protocol.UpipeError {
	return &protocol.UpipeError{Code: protocol.ExitCommandFailed, Kind: kind, Message: message}
}

func batchContextError(err error, stage string) *protocol.UpipeError {
	if err == context.DeadlineExceeded {
		failure := protocol.ErrTimeout("temporary Editor run timed out during " + stage)
		failure.Retryable = false
		return failure
	}
	return protocol.ErrJobCancelled("temporary Editor run cancelled during " + stage)
}

func runBatchEditor(rt *Runtime, unity, logPath string, command SubCommand, args []string) (data any, err error) {
	if d, err := rt.Client.Source(); err == nil && d.Pid > 0 && processAlive(d.Pid) {
		return nil, protocol.ErrBusy("this project already has a running Editor", nil)
	}
	file, err := reserveLog(rt.ProjectPath, logPath)
	if err != nil {
		return nil, usageErr("run: cannot reserve Editor log: " + err.Error())
	}
	out := batchRunResult{LogFile: file, Stage: "startup"}
	var editor *batchEditor
	defer func() {
		if editor != nil {
			out.EditorPID = editor.process.Pid
			out.EditorExitCode = editor.exitCode()
		}
		if err != nil {
			failure := *protocol.AsUpipeError(err)
			out.Result = failure.Data
			failure.Data = out
			failure.Message += " (Editor log: " + file + ")"
			err = &failure
			return
		}
		out.Result = data
		data = out
	}()

	signalled, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	budget := rt.Opts.Timeout
	if budget <= 0 {
		budget = options.DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(signalled, budget)
	defer cancel()
	editor, err = startBatchEditor(unity, rt.ProjectPath, file)
	if err != nil {
		message := "could not start the temporary Editor: " + err.Error()
		if log, openErr := os.OpenFile(file, os.O_WRONLY|os.O_APPEND, 0); openErr == nil {
			_, _ = fmt.Fprintln(log, message)
			_ = log.Close()
		}
		return nil, batchErr("EditorStartupFailed", message)
	}

	client := *rt.Client
	client.Source = editor.ownedSource(rt.Client.Source)
	client.Context = ctx
	client.Sleep = func(delay time.Duration) {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
		}
	}
	ready := false
	defer func() {
		// An independent cleanup budget gives the Editor time to flush its log.
		var quitErr error
		if ready && ctx.Err() == nil && !editor.exited() {
			cleanup, stopCleanup := context.WithTimeout(context.Background(), 5*time.Second)
			quit := client
			quit.Context = cleanup
			_, quitErr = quit.Exec("editor_quit", protocol.ExecOptions{Timeout: time.Second})
			if quitErr == nil {
				select {
				case <-editor.done:
				case <-cleanup.Done():
					quitErr = cleanup.Err()
				}
			}
			stopCleanup()
		}
		closeErr := editor.close()
		select {
		case <-editor.done:
		case <-time.After(5 * time.Second):
			_ = editor.process.Kill()
			select {
			case <-editor.done:
			case <-time.After(time.Second):
				closeErr = fmt.Errorf("the Editor process could not be reaped")
			}
		}
		if err == nil {
			switch {
			case ctx.Err() != nil:
				err = batchContextError(ctx.Err(), out.Stage)
			case quitErr != nil:
				out.Stage = "shutdown"
				err = batchErr("EditorShutdownFailed", "temporary Editor did not exit cleanly: "+quitErr.Error())
			case closeErr != nil:
				out.Stage = "shutdown"
				err = batchErr("EditorShutdownFailed", "could not release the temporary Editor process group: "+closeErr.Error())
			case !editor.exited() || editor.state == nil || !editor.state.Success():
				out.Stage = "shutdown"
				err = batchErr("EditorShutdownFailed", "temporary Editor exited with a nonzero code")
			}
		}
	}()

	if err := editor.waitReady(ctx, &client); err != nil {
		return nil, err
	}
	ready = true
	out.Stage = command.Name
	execution, stopExecution := context.WithCancel(ctx)
	defer stopExecution()
	go func() {
		select {
		case <-editor.done:
			stopExecution()
		case <-execution.Done():
		}
	}()
	client.Context = execution
	nestedOpts := *rt.Opts
	nested := &Runtime{Opts: &nestedOpts, Client: &client, ProjectPath: rt.ProjectPath}
	data, err = command.Run(nested, args)
	if ctx.Err() != nil {
		failure := batchContextError(ctx.Err(), out.Stage)
		if err != nil {
			failure.Data = protocol.AsUpipeError(err).Data
		}
		return nil, failure
	}
	if err != nil && editor.exited() {
		failure := batchErr("EditorExited", "temporary Editor exited while running "+out.Stage)
		failure.Data = protocol.AsUpipeError(err).Data
		return nil, failure
	}
	return data, err
}
