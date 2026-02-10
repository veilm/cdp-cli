package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/veilm/cdp-cli/internal/cdp"
	"github.com/veilm/cdp-cli/internal/store"
)

func cmdScreenshot(args []string) error {
	fs := newFlagSet("screenshot", "usage: cdp screenshot <name> [--selector ...]")
	selector := fs.String("selector", "", "CSS selector to crop")
	output := fs.String("output", "screenshot.png", "Output file path")
	fullPage := fs.Bool("full-page", false, "Capture beyond the current viewport (may cause resize/reflow in headful Chrome)")
	cdpClip := fs.Bool("cdp-clip", false, "When using --selector, crop via CDP clip (may resize/reflow); default is capture viewport then crop locally")
	scrollIntoView := fs.Bool("scroll-into-view", true, "When using --selector (without --cdp-clip), scroll the element into view before capture")
	timeout := fs.Duration("timeout", 15*time.Second, "Command timeout")
	if len(args) == 0 {
		fs.Usage()
		return errors.New("usage: cdp screenshot <name> [--selector ...]")
	}
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 1 {
		return errors.New("usage: cdp screenshot <name> [--selector ...]")
	}
	name := pos[0]
	if len(pos) > 1 {
		return fmt.Errorf("unexpected argument: %s", pos[1])
	}
	if *selector != "" {
		if err := rejectUnsupportedSelector(*selector, "screenshot --selector", false); err != nil {
			return err
		}
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

	params := map[string]interface{}{
		"format":      "png",
		"fromSurface": true,
	}

	// Default to "viewport only" to avoid the headful flicker/resize path in Chromium.
	// `captureBeyondViewport=true` is still available via --full-page (or --cdp-clip).
	params["captureBeyondViewport"] = *fullPage

	var crop *screenshotCrop
	if *selector != "" {
		if *cdpClip {
			clip, err := resolveClip(ctx, handle.client, *selector)
			if err != nil {
				return err
			}
			if clip == nil {
				return fmt.Errorf("selector %s not found", *selector)
			}
			params["clip"] = clip
			params["captureBeyondViewport"] = true
		} else {
			// Compute a viewport-relative crop rect, then crop locally to avoid Chromium resizing the view.
			if *scrollIntoView {
				if err := handle.client.Call(ctx, "DOM.enable", nil, nil); err != nil {
					return err
				}
				nodeID, err := resolveNodeID(ctx, handle.client, *selector)
				if err != nil {
					return err
				}
				if nodeID == 0 {
					return fmt.Errorf("selector %s not found", *selector)
				}
				_ = handle.client.Call(ctx, "DOM.scrollIntoViewIfNeeded", map[string]interface{}{"nodeId": nodeID}, nil)
			}
			var err error
			crop, err = resolveViewportCrop(ctx, handle.client, *selector)
			if err != nil {
				return err
			}
			if crop == nil {
				return fmt.Errorf("selector %s not found", *selector)
			}
		}
	}

	var shot struct {
		Data string `json:"data"`
	}
	if err := handle.client.Call(ctx, "Page.captureScreenshot", params, &shot); err != nil {
		return err
	}
	data, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil {
		return err
	}

	if crop != nil {
		cropped, err := cropPNG(data, *crop)
		if err != nil {
			return err
		}
		data = cropped
	}

	if err := os.WriteFile(*output, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("Saved %s (%d bytes)\n", *output, len(data))
	return nil
}

type screenshotCrop struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
	DPR    float64
}

func resolveViewportCrop(ctx context.Context, client *cdp.Client, selector string) (*screenshotCrop, error) {
	expression := fmt.Sprintf(`(() => {
        const el = document.querySelector(%s);
        if (!el) { return null; }
        const r = el.getBoundingClientRect();
        const dpr = window.devicePixelRatio || 1;
        return {
            x: r.left,
            y: r.top,
            width: r.width,
            height: r.height,
            dpr
        };
    })()`, strconv.Quote(selector))
	value, err := client.Evaluate(ctx, expression)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	m, ok := value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected crop result type %T", value)
	}
	crop := &screenshotCrop{}
	if v, ok := m["x"].(float64); ok {
		crop.X = v
	}
	if v, ok := m["y"].(float64); ok {
		crop.Y = v
	}
	if v, ok := m["width"].(float64); ok {
		crop.Width = v
	}
	if v, ok := m["height"].(float64); ok {
		crop.Height = v
	}
	if v, ok := m["dpr"].(float64); ok {
		crop.DPR = v
	}
	return crop, nil
}

func cropPNG(pngBytes []byte, crop screenshotCrop) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	left := int(math.Round(crop.X * crop.DPR))
	top := int(math.Round(crop.Y * crop.DPR))
	right := int(math.Round((crop.X + crop.Width) * crop.DPR))
	bottom := int(math.Round((crop.Y + crop.Height) * crop.DPR))

	left = clampInt(left, bounds.Min.X, bounds.Max.X)
	top = clampInt(top, bounds.Min.Y, bounds.Max.Y)
	right = clampInt(right, bounds.Min.X, bounds.Max.X)
	bottom = clampInt(bottom, bounds.Min.Y, bounds.Max.Y)
	if right <= left || bottom <= top {
		return nil, fmt.Errorf("crop rectangle is empty after clamping (x=%d y=%d w=%d h=%d)", left, top, right-left, bottom-top)
	}
	if left == bounds.Min.X && top == bounds.Min.Y && right == bounds.Max.X && bottom == bounds.Max.Y {
		return pngBytes, nil
	}
	r := image.Rect(left, top, right, bottom)
	cropper, ok := img.(interface {
		SubImage(r image.Rectangle) image.Image
	})
	if !ok {
		return nil, errors.New("image does not support cropping")
	}
	sub := cropper.SubImage(r)

	var buf bytes.Buffer
	if err := png.Encode(&buf, sub); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func resolveClip(ctx context.Context, client *cdp.Client, selector string) (map[string]interface{}, error) {
	var doc struct {
		Root struct {
			NodeID int `json:"nodeId"`
		} `json:"root"`
	}
	if err := client.Call(ctx, "DOM.getDocument", map[string]interface{}{"depth": 1}, &doc); err != nil {
		return nil, err
	}
	if doc.Root.NodeID == 0 {
		return nil, errors.New("DOM.getDocument returned empty root")
	}
	var node struct {
		NodeID int `json:"nodeId"`
	}
	if err := client.Call(ctx, "DOM.querySelector", map[string]interface{}{
		"nodeId":   doc.Root.NodeID,
		"selector": selector,
	}, &node); err != nil {
		return nil, err
	}
	if node.NodeID == 0 {
		return nil, nil
	}
	var box struct {
		Model struct {
			Content []float64 `json:"content"`
		} `json:"model"`
	}
	if err := client.Call(ctx, "DOM.getBoxModel", map[string]interface{}{"nodeId": node.NodeID}, &box); err != nil {
		return nil, err
	}
	if len(box.Model.Content) < 8 {
		return nil, errors.New("DOM.getBoxModel returned invalid content box")
	}
	content := box.Model.Content
	left := content[0]
	top := content[1]
	right := content[4]
	bottom := content[5]
	if right < left {
		left, right = right, left
	}
	if bottom < top {
		top, bottom = bottom, top
	}
	width := right - left
	height := bottom - top
	if width <= 0 || height <= 0 {
		return nil, errors.New("clip is empty")
	}

	return map[string]interface{}{
		"x":      left,
		"y":      top,
		"width":  width,
		"height": height,
		"scale":  1,
	}, nil
}

func resolveNodeID(ctx context.Context, client *cdp.Client, selector string) (int, error) {
	var doc struct {
		Root struct {
			NodeID int `json:"nodeId"`
		} `json:"root"`
	}
	if err := client.Call(ctx, "DOM.getDocument", map[string]interface{}{"depth": 1}, &doc); err != nil {
		return 0, err
	}
	if doc.Root.NodeID == 0 {
		return 0, nil
	}
	var node struct {
		NodeID int `json:"nodeId"`
	}
	if err := client.Call(ctx, "DOM.querySelector", map[string]interface{}{
		"nodeId":   doc.Root.NodeID,
		"selector": selector,
	}, &node); err != nil {
		return 0, err
	}
	return node.NodeID, nil
}

func clampInt(val, min, max int) int {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}
