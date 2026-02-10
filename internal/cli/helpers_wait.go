package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/veilm/cdp-cli/internal/cdp"
)

func waitForReadyState(ctx context.Context, client *cdp.Client, poll time.Duration) error {
	return waitForCondition(ctx, client, `document.readyState === "complete"`, "document.readyState == 'complete'", poll)
}

func waitForSelector(ctx context.Context, client *cdp.Client, selector string, poll time.Duration) error {
	expression := fmt.Sprintf(`(() => {
        return document.querySelector(%s) !== null;
    })()`, strconv.Quote(selector))
	return waitForCondition(ctx, client, expression, fmt.Sprintf("selector %s", selector), poll)
}

func waitForSelectorVisible(ctx context.Context, client *cdp.Client, selector string, poll time.Duration) error {
	expression := fmt.Sprintf(`(() => {
        const el = document.querySelector(%s);
        if (!el) { return false; }
        const style = window.getComputedStyle(el);
        if (style && (style.display === "none" || style.visibility === "hidden" || style.opacity === "0")) {
            return false;
        }
        const rect = el.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0;
    })()`, strconv.Quote(selector))
	return waitForCondition(ctx, client, expression, fmt.Sprintf("visible selector %s", selector), poll)
}

func waitForCondition(ctx context.Context, client *cdp.Client, expression, description string, poll time.Duration) error {
	if poll <= 0 {
		poll = 200 * time.Millisecond
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		ok, err := evalBool(ctx, client, expression)
		if err == nil && ok {
			return nil
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("timeout waiting for %s", description)
			}
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func evalBool(ctx context.Context, client *cdp.Client, expression string) (bool, error) {
	value, err := client.Evaluate(ctx, expression)
	if err != nil {
		return false, err
	}
	result, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("unexpected wait result type %T", value)
	}
	return result, nil
}
