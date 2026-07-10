package cli

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/veilm/cdp-cli/internal/cdp"
	"github.com/veilm/cdp-cli/internal/store"
)

type emulationSettings struct {
	name   string
	width  int
	height int
	dpr    float64
	mobile bool
	touch  bool
	reopen bool
}

var customDevicePattern = regexp.MustCompile(`^(\d+)[xX](\d+)$`)

func cmdEmulate(args []string) error {
	fs := newFlagSet("emulate", "usage: cdp emulate --session <name> <phone|tablet|WIDTHxHEIGHT|refresh-reset> [--mobile] [--dpr N]")
	sessionFlag := addSessionFlag(fs)
	mobile := fs.Bool("mobile", false, "Use mobile metrics and touch for a custom size")
	dpr := fs.Float64("dpr", 1, "Device pixel ratio")
	timeout := fs.Duration("timeout", 10*time.Second, "Command timeout")
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		fs.Usage()
		return errors.New("missing device type")
	}
	if len(pos) > 1 {
		return fmt.Errorf("unexpected argument: %s", pos[1])
	}
	settings, err := parseEmulationSettings(pos[0], *mobile, *dpr)
	if err != nil {
		return err
	}
	name, err := resolveSessionName(*sessionFlag)
	if err != nil {
		fs.Usage()
		return err
	}

	st, err := store.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	handle, err := openSession(ctx, st, name)
	if err != nil {
		return err
	}
	defer handle.Close()

	if settings.reopen {
		return reopenForReset(ctx, handle)
	}

	if err := applyEmulation(ctx, handle.client, settings); err != nil {
		return err
	}
	viewport, err := readViewport(ctx, handle.client)
	if err != nil {
		return fmt.Errorf("verify emulation: %w", err)
	}
	mode := "desktop"
	if settings.mobile {
		mode = "mobile, touch"
	}
	fmt.Printf("Emulating %s: %.0fx%.0f, DPR %.6g, %s (%s)\n", settings.name, viewport.Width, viewport.Height, viewport.DPR, mode, viewport.URL)
	return nil
}

func parseEmulationSettings(device string, mobile bool, dpr float64) (emulationSettings, error) {
	if dpr <= 0 {
		return emulationSettings{}, errors.New("--dpr must be greater than zero")
	}
	switch device {
	case "phone":
		return emulationSettings{name: device, width: 390, height: 844, dpr: dpr, mobile: true, touch: true}, nil
	case "tablet":
		return emulationSettings{name: device, width: 820, height: 1180, dpr: dpr, mobile: true, touch: true}, nil
	case "refresh-reset":
		return emulationSettings{name: "refresh-reset", reopen: true}, nil
	}
	match := customDevicePattern.FindStringSubmatch(device)
	if match == nil {
		return emulationSettings{}, fmt.Errorf("unknown device type %q (expected phone, tablet, WIDTHxHEIGHT, or refresh-reset)", device)
	}
	width, _ := strconv.Atoi(match[1])
	height, _ := strconv.Atoi(match[2])
	if width == 0 || height == 0 {
		return emulationSettings{}, errors.New("device width and height must be greater than zero")
	}
	return emulationSettings{name: device, width: width, height: height, dpr: dpr, mobile: mobile, touch: mobile}, nil
}

func applyEmulation(ctx context.Context, client *cdp.Client, settings emulationSettings) error {
	params := map[string]interface{}{
		"width": settings.width, "height": settings.height,
		"deviceScaleFactor": settings.dpr, "mobile": settings.mobile,
		"screenWidth": settings.width, "screenHeight": settings.height,
	}
	if err := client.Call(ctx, "Emulation.setDeviceMetricsOverride", params, nil); err != nil {
		return err
	}
	return client.Call(ctx, "Emulation.setTouchEmulationEnabled", map[string]interface{}{
		"enabled": settings.touch, "maxTouchPoints": map[bool]int{true: 5, false: 1}[settings.touch],
	}, nil)
}

type viewportInfo struct {
	URL         string  `json:"url"`
	Width       float64 `json:"width"`
	Height      float64 `json:"height"`
	OuterWidth  float64 `json:"outerWidth"`
	OuterHeight float64 `json:"outerHeight"`
	DPR         float64 `json:"dpr"`
}

func readViewport(ctx context.Context, client *cdp.Client) (viewportInfo, error) {
	var response struct {
		Result struct {
			Value viewportInfo `json:"value"`
		} `json:"result"`
	}
	err := client.Call(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression":    "({url: location.href, width: innerWidth, height: innerHeight, outerWidth, outerHeight, dpr: devicePixelRatio})",
		"returnByValue": true,
	}, &response)
	return response.Result.Value, err
}

func reopenForReset(ctx context.Context, handle *sessionHandle) error {
	oldTargetID := handle.session.TargetID
	fresh, err := cdp.CreateTarget(ctx, handle.session.Host, handle.session.Port, handle.session.URL)
	if err != nil {
		return fmt.Errorf("open replacement tab: %w", err)
	}
	freshWebSocketURL := rewriteWebSocketURL(fresh.WebSocket, handle.session.Host, handle.session.Port)
	freshClient, err := cdp.Dial(ctx, freshWebSocketURL)
	if err != nil {
		return fmt.Errorf("connect to replacement tab: %w", err)
	}
	if err := waitForReadyState(ctx, freshClient, 100*time.Millisecond); err != nil {
		freshClient.Close()
		return fmt.Errorf("wait for replacement tab: %w", err)
	}
	freshClient.Close()
	if err := cdp.ActivateTarget(ctx, handle.session.Host, handle.session.Port, fresh.ID); err != nil {
		return fmt.Errorf("activate replacement tab: %w", err)
	}
	handle.session.TargetID = fresh.ID
	handle.session.WebSocketURL = freshWebSocketURL
	handle.session.URL = fresh.URL
	handle.session.Title = fresh.Title
	handle.session.Type = fresh.Type
	if err := cdp.CloseTarget(ctx, handle.session.Host, handle.session.Port, oldTargetID); err != nil {
		fmt.Printf("warning: replacement tab is active but old tab could not be closed: %v\n", err)
	}
	fmt.Printf("Refresh-reset reopened %s; temporary page state was discarded\n", handle.session.URL)
	return nil
}
