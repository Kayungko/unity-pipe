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
		asset := fs.String("asset", "", "saved Prefab, Sprite or Material path under Assets/")
		kind := fs.String("replacement-kind", "", "package a replacement: sprite or material")
		path := fs.String("output-dir", "", "new bundle directory outside the Unity project")
		confirm := fs.Bool("confirm", false, "allow packaging the asset")
		rest, err := parseArgs(fs, args)
		if err != nil || len(rest) != 0 || *asset == "" || *path == "" || !*confirm {
			return nil, usageErr("preview-pack: --asset, --output-dir and --confirm are required")
		}
		dir, err := filepath.Abs(*path)
		if err != nil {
			return nil, usageErr("preview-pack: " + err.Error())
		}
		params := map[string]any{"asset": *asset, "output_dir": dir, "confirm": true}
		if *kind != "" {
			if *kind != "sprite" && *kind != "material" {
				return nil, usageErr("preview-pack: --replacement-kind must be sprite or material")
			}
			params["replacement_kind"] = *kind
		}
		return ExecCommand().Run(rt, []string{"pack_scene_preview", "--params", string(mustMarshal(params))})
	}}
}

type previewMetadata struct {
	Schema          int    `json:"schema"`
	Target          string `json:"target"`
	UnityVersion    string `json:"unityVersion"`
	ProjectGUID     string `json:"projectGuid"`
	ReplacementKind string `json:"replacementKind"`
}

type previewReplacement struct {
	Node         string
	BeforePath   string
	MaterialSlot int
}

type previewResult struct {
	LogFile         string          `json:"logFile,omitempty"`
	Stage           string          `json:"stage"`
	PlayerPID       int             `json:"playerPid,omitempty"`
	PlayerExitCode  *int            `json:"playerExitCode,omitempty"`
	ImagePath       string          `json:"imagePath,omitempty"`
	BeforeImagePath string          `json:"beforeImagePath,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
}

func PreviewCommand() SubCommand {
	return SubCommand{
		Name: "preview", NoRuntime: true,
		Synopsis: "render a Prefab or preview a scene replacement in a dedicated Windows Development Player",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe preview")
			executable := fs.String("player-exe", "", "dedicated preview Player executable")
			bundle := fs.String("bundle-dir", "", "directory returned by run -- preview-pack")
			image := fs.String("output-path", "", "new PNG path")
			node := fs.String("node", "", "absolute scene hierarchy path for replacement (e.g. /Stage/Target)")
			before := fs.String("before-path", "", "new PNG path for the scene before replacement")
			slot := fs.Int("material-slot", 0, "zero-based Renderer material slot")
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
			if (*node == "") != (*before == "") || *slot < 0 || (*node == "" && *slot != 0) ||
				(*node != "" && (!strings.HasPrefix(*node, "/") || strings.HasSuffix(*node, "/") || strings.Contains(*node, "//"))) {
				return nil, usageErr("preview: use an absolute --node with --before-path and a nonnegative material slot")
			}
			replacement := previewReplacement{Node: *node, BeforePath: *before, MaterialSlot: *slot}
			return renderPreview(*executable, *bundle, *image, *log, *width, *height, rt.Opts.Timeout, replacement)
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

func renderPreview(executable, bundle, image, log string, width, height int, budget time.Duration, replacement previewReplacement) (any, error) {
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
	if host.Target != pack.Target || host.UnityVersion != pack.UnityVersion || host.ProjectGUID != pack.ProjectGUID {
		return failed("preview Player and package differ in target, Unity version or project")
	}
	if (pack.ReplacementKind == "") != (replacement.Node == "") {
		return failed("a scene replacement requires a Sprite or Material bundle and a target node")
	}
	switch pack.ReplacementKind {
	case "", "material":
	case "sprite":
		if replacement.MaterialSlot != 0 {
			return nil, usageErr("preview: material slots only apply to Material replacement")
		}
	default:
		return failed("unknown replacement kind in the preview package")
	}
	if _, err := os.Lstat(image); !os.IsNotExist(err) {
		return failed("PNG output path must be new")
	}
	if log == "" {
		log = image + ".player.log"
	}
	if replacement.Node != "" {
		before, err := filepath.Abs(replacement.BeforePath)
		if err != nil || !strings.EqualFold(filepath.Ext(before), ".png") || strings.EqualFold(before, image) {
			return nil, usageErr("preview: --before-path must be a distinct .png path")
		}
		logPath, err := filepath.Abs(log)
		if err == nil && (strings.EqualFold(logPath, before) || strings.EqualFold(logPath, image)) {
			return nil, usageErr("preview: the Player log needs a separate path")
		}
		if _, err := os.Lstat(before); !os.IsNotExist(err) {
			return failed("before PNG output path must be new")
		}
		replacement.BeforePath = before
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
	playerArgs := []string{"-batchmode", "-force-d3d11", "-logFile", out.LogFile,
		"-screen-width", imageWidth, "-screen-height", imageHeight, "-screen-fullscreen", "0",
		"-upipe-scene-preview", bundle, staging, imageWidth, imageHeight}
	if replacement.Node != "" {
		playerArgs = append(playerArgs, replacement.Node, strconv.Itoa(replacement.MaterialSlot))
	}
	cmd := exec.Command(executable, playerArgs...)
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
		Success         bool   `json:"success"`
		Message         string `json:"message"`
		Stage           string `json:"stage"`
		Node            string `json:"node"`
		ReplacementKind string `json:"replacementKind"`
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
	if receipt.Node != replacement.Node || receipt.ReplacementKind != pack.ReplacementKind {
		return failed("preview result differs from the requested replacement; rebuild the preview Player")
	}
	out.Stage = "output"
	pixels, err := readPreviewPNG(staging, "preview.png", width, height)
	if err != nil {
		return failed(err.Error())
	}
	if replacement.Node != "" {
		before, err := readPreviewPNG(staging, "before.png", width, height)
		if err != nil {
			return failed(err.Error())
		}
		if err := writePreviewPNG(replacement.BeforePath, before); err != nil {
			return failed(err.Error())
		}
	}
	if err := writePreviewPNG(image, pixels); err != nil {
		if replacement.Node != "" {
			err = errors.Join(err, os.Remove(replacement.BeforePath))
		}
		return failed(err.Error())
	}
	out.ImagePath, out.BeforeImagePath, out.Stage = image, replacement.BeforePath, "completed"
	return out, nil
}

func readPreviewPNG(staging, name string, width, height int) ([]byte, error) {
	pixels, err := os.ReadFile(filepath.Join(staging, name))
	if err != nil {
		return nil, fmt.Errorf("preview Player did not produce %s: %w", name, err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(pixels))
	if err != nil {
		return nil, fmt.Errorf("preview Player produced an invalid PNG: %s", name)
	}
	if config.Width != width || config.Height != height {
		return nil, fmt.Errorf("Player produced %s at %dx%d instead of %dx%d; rebuild the preview Player",
			name, config.Width, config.Height, width, height)
	}
	return pixels, nil
}

func writePreviewPNG(path string, pixels []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("could not create the PNG: %w", err)
	}
	_, writeErr := file.Write(pixels)
	if err = errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("could not save the PNG: %w", errors.Join(err, os.Remove(path)))
	}
	return nil
}
