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
	reset  bool
}

var customDevicePattern = regexp.MustCompile(`^(\d+)[xX](\d+)$`)

func cmdEmulate(args []string) error {
	fs := newFlagSet("emulate", "usage: cdp emulate --session <name> <phone|tablet|WIDTHxHEIGHT|reset> [--mobile] [--dpr N]")
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

	if err := applyEmulation(ctx, handle.client, settings); err != nil {
		return err
	}
	viewport, err := readViewport(ctx, handle.client)
	if err != nil {
		return fmt.Errorf("verify emulation: %w", err)
	}
	if settings.reset {
		fmt.Printf("Device emulation reset: %.0fx%.0f, DPR %.6g (%s)\n", viewport.Width, viewport.Height, viewport.DPR, viewport.URL)
		return nil
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
	case "reset", "desktop":
		return emulationSettings{name: device, reset: true}, nil
	}
	match := customDevicePattern.FindStringSubmatch(device)
	if match == nil {
		return emulationSettings{}, fmt.Errorf("unknown device type %q (expected phone, tablet, WIDTHxHEIGHT, or reset)", device)
	}
	width, _ := strconv.Atoi(match[1])
	height, _ := strconv.Atoi(match[2])
	if width == 0 || height == 0 {
		return emulationSettings{}, errors.New("device width and height must be greater than zero")
	}
	return emulationSettings{name: device, width: width, height: height, dpr: dpr, mobile: mobile, touch: mobile}, nil
}

func applyEmulation(ctx context.Context, client *cdp.Client, settings emulationSettings) error {
	if settings.reset {
		if err := client.Call(ctx, "Emulation.clearDeviceMetricsOverride", nil, nil); err != nil {
			return err
		}
		return client.Call(ctx, "Emulation.setTouchEmulationEnabled", map[string]interface{}{"enabled": false}, nil)
	}
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
	URL    string  `json:"url"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	DPR    float64 `json:"dpr"`
}

func readViewport(ctx context.Context, client *cdp.Client) (viewportInfo, error) {
	var response struct {
		Result struct {
			Value viewportInfo `json:"value"`
		} `json:"result"`
	}
	err := client.Call(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression":    "({url: location.href, width: innerWidth, height: innerHeight, dpr: devicePixelRatio})",
		"returnByValue": true,
	}, &response)
	return response.Result.Value, err
}
