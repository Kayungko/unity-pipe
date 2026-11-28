package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// BuildCommand waits for the report from the particular build it queued.
func BuildCommand() SubCommand {
	return SubCommand{
		Name:     "build",
		Synopsis: "build a Player in the running Editor and wait for its report [--dry-run|--confirm] [--no-wait]",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe build")
			target := fs.String("target", "", "Unity BuildTarget name (default: active target)")
			output := fs.String("output-path", "", "Player output path, relative to the project or absolute")
			config := fs.String("config", "", "named configuration from the project\x27s .upipe/builds.json")
			profile := fs.String("profile", "", "Build Profile name (requires Unity 6)")
			preview := fs.String("scene-preview-bundle", "", "build a dedicated Development Player for this preview bundle directory")
			confirm := fs.Bool("confirm", false, "allow the Editor to build the Player")
			dryRun := fs.Bool("dry-run", false, "validate and return the resolved build plan without building")
			noWait := fs.Bool("no-wait", false, "return the queued build ID instead of waiting for a report")
			var scenes, buildOptions multiFlag
			fs.Var(&scenes, "scene", "scene asset path (repeatable)")
			fs.Var(&buildOptions, "option", "BuildOptions enum name (repeatable)")
			attachShared(fs, rt.Opts)
			rest, err := parseArgs(fs, args)
			if err != nil {
				return nil, usageErr("build: " + err.Error())
			}
			if len(rest) != 0 {
				return nil, usageErr("build: unexpected positional arguments")
			}
			var configSet bool
			var conflictingFlags []string
			fs.Visit(func(f *flag.Flag) {
				switch f.Name {
				case "config":
					configSet = true
				case "target", "output-path", "scene", "option", "profile", "scene-preview-bundle":
					conflictingFlags = append(conflictingFlags, "--"+f.Name)
				}
			})
			if configSet {
				if len(conflictingFlags) > 0 {
					return nil, usageErr("build: --config cannot be combined with " + strings.Join(conflictingFlags, ", "))
				}
				build, err := loadNamedBuild(rt.ProjectPath, *config)
				if err != nil {
					return nil, usageErr("build: " + err.Error())
				}
				*target, *output = build.Target, build.OutputPath
				scenes, buildOptions = build.Scenes, build.Options
			}
			params := map[string]any{
				"target": *target, "outputPath": *output, "profileName": *profile,
				"confirm": *confirm, "dry_run": *dryRun,
			}
			if len(scenes) != 0 {
				params["scenes"] = scenes
			}
			if len(buildOptions) != 0 {
				params["options"] = buildOptions
			}
			if *preview != "" {
				manifestDir, err := filepath.Abs(*preview)
				if err != nil {
					return nil, usageErr("build: invalid preview bundle directory: " + err.Error())
				}
				params["scenePreviewBundle"] = manifestDir
			}
			// The Editor may already have queued the build if the reply gets lost.
			reply, err := rt.Client.Exec("build", protocol.ExecOptions{Parameters: params, Timeout: rt.Opts.Timeout})
			if err != nil {
				return nil, err
			}
			body, state, err := parseBuildState(reply.Result)
			if err != nil {
				return nil, err
			}
			switch state.Status {
			case "dry_run":
				if state.Valid == nil {
					return nil, protocol.ErrProtocol("build dry run did not report validation")
				}
				if !*state.Valid {
					return nil, buildFailure(body, "build configuration is invalid")
				}
				return body, nil
			case "error":
				return nil, buildFailure(body, firstNonEmpty(state.Message, "build was refused"))
			case "busy":
				return nil, protocol.ErrBusy(firstNonEmpty(state.Message, "build is busy"), body)
			case "queued":
				if state.BuildID == "" {
					return nil, protocol.ErrProtocol("queued build did not return an ID")
				}
				if *noWait {
					return body, nil
				}
				return rt.waitForBuild(state.BuildID)
			default:
				return nil, protocol.ErrProtocol(fmt.Sprintf("unexpected build submission status %q", state.Status))
			}
		},
	}
}

type buildState struct {
	Status  string
	BuildID string
	Result  string
	Message string
	Valid   *bool
}

func parseBuildState(raw json.RawMessage) (json.RawMessage, buildState, error) {
	body := protocol.UnwrapJSONString(raw)
	var state buildState
	if err := json.Unmarshal(body, &state); err != nil || state.Status == "" {
		return nil, state, protocol.ErrProtocol("invalid build response")
	}
	return body, state, nil
}

func (rt *Runtime) waitForBuild(id string) (any, error) {
	var lastBody json.RawMessage
	result, err := protocol.Poll(func() (protocol.PollStep, error) {
		reply, err := rt.Client.Exec("build_status", protocol.ExecOptions{Timeout: 3 * time.Second})
		if err != nil {
			return protocol.PollStep{}, err
		}
		body, status, err := parseBuildState(reply.Result)
		if err != nil {
			return protocol.PollStep{}, err
		}
		if status.BuildID != id {
			return protocol.PollStep{}, buildFailure(body, fmt.Sprintf("build status for %s is no longer available", id))
		}
		lastBody = body
		switch status.Status {
		case "queued", "building":
		case "interrupted":
			return protocol.PollStep{}, buildFailure(body, firstNonEmpty(status.Message, "build interrupted"))
		case "completed":
			switch strings.ToLower(status.Result) {
			case "succeeded":
			case "cancelled", "canceled":
				failure := protocol.ErrJobCancelled("Unity cancelled the build")
				failure.Data = body
				return protocol.PollStep{}, failure
			case "failed", "unknown":
				return protocol.PollStep{}, buildFailure(body, "Unity Player build failed")
			default:
				return protocol.PollStep{}, protocol.ErrProtocol(fmt.Sprintf("unexpected Unity build result %q", status.Result))
			}
		default:
			return protocol.PollStep{}, protocol.ErrProtocol(fmt.Sprintf("unexpected build status %q", status.Status))
		}
		return protocol.PollStep{Status: status.Status, Body: body}, nil
	}, protocol.PollConfig{TerminalOK: []string{"completed"}, Deadline: rt.Opts.Timeout}, nil, nil)
	if err != nil {
		failure := protocol.AsUpipeError(err)
		if rt.Client.Context != nil && rt.Client.Context.Err() != nil &&
			(failure.Code == protocol.ExitTimeout || failure.Code == protocol.ExitJobCancelled) {
			failure.Data = map[string]any{"buildId": id, "lastStatus": lastBody, "outcome": "unknown"}
		} else if failure.Code == protocol.ExitTimeout {
			failure.Data = map[string]any{"buildId": id, "lastStatus": lastBody}
			failure.Message += "; the Editor may still be building"
		}
		return nil, failure
	}
	return result.Body, nil
}

func buildFailure(body json.RawMessage, message string) error {
	return &protocol.UpipeError{Code: protocol.ExitCommandFailed, Kind: "CommandFailed", Message: message, Data: body}
}
