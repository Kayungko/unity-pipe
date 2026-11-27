package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

func previewPackCommand() SubCommand {
	return SubCommand{Name: "preview-pack", Run: func(rt *Runtime, args []string) (any, error) {
		fs := newFlagSet("upipe run -- preview-pack")
		asset := fs.String("asset", "", "saved Prefab path under Assets/")
		path := fs.String("output-dir", "", "new bundle directory outside the Unity project")
		confirm := fs.Bool("confirm", false, "allow packaging the Prefab")
		rest, err := parseArgs(fs, args)
		if err != nil || len(rest) != 0 || *asset == "" || *path == "" || !*confirm {
			return nil, usageErr("preview-pack: --asset, --output-dir and --confirm are required")
		}
		dir, err := filepath.Abs(*path)
		if err != nil {
			return nil, usageErr("preview-pack: " + err.Error())
		}
		return ExecCommand().Run(rt, []string{"pack_scene_preview", "--params",
			string(mustMarshal(map[string]any{"asset": *asset, "output_dir": dir, "confirm": true}))})
	}}
}

type previewMetadata struct {
	Schema       int    `json:"schema"`
	Target       string `json:"target"`
	UnityVersion string `json:"unityVersion"`
	ProjectGUID  string `json:"projectGuid"`
}

type previewResult struct {
	LogFile        string          `json:"logFile,omitempty"`
	Stage          string          `json:"stage"`
	PlayerPID      int             `json:"playerPid,omitempty"`
	PlayerExitCode *int            `json:"playerExitCode,omitempty"`
	ImagePath      string          `json:"imagePath,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
}

func PreviewCommand() SubCommand {
	return SubCommand{
		Name: "preview", NoRuntime: true,
		Synopsis: "render a packed Prefab to a PNG in a dedicated Windows Development Player",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe preview")
			executable := fs.String("player-exe", "", "dedicated preview Player executable")
			bundle := fs.String("bundle-dir", "", "directory returned by run -- preview-pack")
			image := fs.String("output-path", "", "new PNG path")
			width := fs.Int("width", 768, "PNG width in pixels (64–4096)")
			height := fs.Int("height", 768, "PNG height in pixels (64–4096)")
			log := fs.String("log-file", "", "new Player log path (default: <output-path>.player.log)")
			attachShared(fs, rt.Opts)
			rest, err := parseArgs(fs, args)
			if err != nil || len(rest) != 0 || *executable == "" || *bundle == "" || *image == "" ||
				rt.Opts.Timeout < 0 || rt.Opts.ProjectPath != "" || rt.Opts.PlayerPath != "" {
				return nil, usageErr("preview: --player-exe, --bundle-dir and --output-path are required")
			}
			if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
				return nil, usageErr("preview: Windows x64 is required")
			}
			if *width < 64 || *width > 4096 || *height < 64 || *height > 4096 {
				return nil, usageErr("preview: width and height must be between 64 and 4096")
			}
			return renderPreview(*executable, *bundle, *image, *log, *width, *height, rt.Opts.Timeout)
		},
	}
}

func readPreviewMetadata(path string) (previewMetadata, error) {
	var metadata previewMetadata
	data, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &metadata)
	}
	if err == nil && (metadata.Schema != 1 || metadata.Target != "StandaloneWindows64" ||
		metadata.UnityVersion == "" || metadata.ProjectGUID == "") {
		err = errors.New("expected Windows x64 preview metadata (schema 1)")
	}
	return metadata, err
}

func renderPreview(executable, bundle, image, log string, width, height int, budget time.Duration) (any, error) {
	out := previewResult{Stage: "validate"}
	failed := func(message string) (any, error) {
		e := batchErr("ScenePreviewFailed", message)
		e.Data = out
		return nil, e
	}
	executable, err := filepath.Abs(executable)
	if err != nil || !strings.EqualFold(filepath.Ext(executable), ".exe") {
		return nil, usageErr("preview: expected a valid .exe path")
	}
	bundle, err = filepath.Abs(bundle)
	if err != nil {
		return nil, usageErr("preview: invalid bundle path")
	}
	image, err = filepath.Abs(image)
	if err != nil || !strings.EqualFold(filepath.Ext(image), ".png") {
		return nil, usageErr("preview: expected a valid .png output path")
	}
	host, err := readPreviewMetadata(executable + ".scene-preview.json")
	if err != nil {
		return failed("could not read dedicated preview Player metadata: " + err.Error())
	}
	pack, err := readPreviewMetadata(filepath.Join(bundle, "preview-package.json"))
	if err != nil {
		return failed("could not read preview package metadata: " + err.Error())
	}
	if host != pack {
		return failed("preview Player and package differ in target, Unity version or project")
	}
	if _, err := os.Lstat(image); !os.IsNotExist(err) {
		return failed("PNG output path must be new")
	}
	if log == "" {
		log = image + ".player.log"
	}
	out.LogFile, err = reserveLog(filepath.Dir(image), log)
	if err != nil {
		return failed("could not reserve the Player log: " + err.Error())
	}

	staging, err := os.MkdirTemp(filepath.Dir(image), ".upipe-preview-")
	if err != nil {
		return failed("could not create staging directory: " + err.Error())
	}
	defer os.RemoveAll(staging)

	signalled, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if budget == 0 {
		budget = time.Minute
	}
	ctx, cancel := context.WithTimeout(signalled, budget)
	defer cancel()

	out.Stage = "startup"
	imageWidth, imageHeight := strconv.Itoa(width), strconv.Itoa(height)
	cmd := exec.Command(executable, "-batchmode", "-force-d3d11", "-logFile", out.LogFile,
		"-screen-width", imageWidth, "-screen-height", imageHeight, "-screen-fullscreen", "0",
		"-upipe-scene-preview", bundle, staging, imageWidth, imageHeight)
	cmd.Dir = staging
	closeScope, err := startScopedEditor(cmd)
	if err != nil {
		return failed("could not start the preview Player: " + err.Error())
	}
	out.PlayerPID = cmd.Process.Pid
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
		if closeErr := closeScope(); closeErr != nil {
			return failed("could not release the preview Player process scope: " + closeErr.Error())
		}
	case <-ctx.Done():
		if closeErr := closeScope(); closeErr != nil {
			_ = cmd.Process.Kill()
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			return failed("could not reap the preview Player")
		}
	}
	code := cmd.ProcessState.ExitCode()
	out.PlayerExitCode = &code
	out.Stage = "player"
	if ctx.Err() != nil {
		e := protocol.ErrJobCancelled("preview Player cancelled")
		if ctx.Err() == context.DeadlineExceeded {
			e = protocol.ErrTimeout("preview Player timed out")
			e.Retryable = false
		}
		e.Data = out
		return nil, e
	}

	data, readErr := os.ReadFile(filepath.Join(staging, "result.json"))
	var receipt struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Stage   string `json:"stage"`
	}
	if readErr != nil || json.Unmarshal(data, &receipt) != nil || receipt.Stage == "" {
		return failed("preview Player exited without a valid result; check the Player log")
	}
	out.Result, out.Stage = data, receipt.Stage
	if !receipt.Success {
		return failed(receipt.Message)
	}
	if err != nil || code != 0 || receipt.Stage != "completed" {
		return failed("preview Player did not complete cleanly")
	}
	out.Stage = "output"
	pixels, err := os.ReadFile(filepath.Join(staging, "preview.png"))
	if err != nil {
		return failed("preview Player did not produce a PNG: " + err.Error())
	}
	config, err := png.DecodeConfig(bytes.NewReader(pixels))
	if err != nil {
		return failed("preview Player produced an invalid PNG")
	}
	if config.Width != width || config.Height != height {
		return failed(fmt.Sprintf("Player produced %dx%d PNG instead of %dx%d; rebuild the preview Player",
			config.Width, config.Height, width, height))
	}
	file, err := os.OpenFile(image, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return failed("could not create the PNG: " + err.Error())
	}
	_, writeErr := file.Write(pixels)
	if err = errors.Join(writeErr, file.Close()); err != nil {
		_ = os.Remove(image)
		return failed("could not save the PNG: " + err.Error())
	}
	out.ImagePath, out.Stage = image, "completed"
	return out, nil
}
