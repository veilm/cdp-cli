package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/veilm/cdp-cli/internal/cdp"
)

type pageIssue struct {
	Code    string
	Heading string
	Summary string
}

func fetchLatestTargetInfo(ctx context.Context, host string, port int, target cdp.TargetInfo) (cdp.TargetInfo, error) {
	targets, err := cdp.ListTargets(ctx, host, port)
	if err != nil {
		return target, err
	}
	for _, candidate := range targets {
		if candidate.ID == target.ID {
			return candidate, nil
		}
	}
	if target.URL != "" {
		if candidate, ok := cdp.FindTarget(targets, target.URL); ok {
			return candidate, nil
		}
	}
	return target, nil
}

func detectPageIssue(ctx context.Context, wsURL string) (*pageIssue, error) {
	if strings.TrimSpace(wsURL) == "" {
		return nil, nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()

	client, err := cdp.Dial(checkCtx, wsURL)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	for {
		value, err := client.Evaluate(checkCtx, `(() => {
			const data = globalThis.loadTimeDataRaw;
			const issue = (() => {
				if (!data || typeof data !== "object") {
					return null;
				}
				const code = typeof data.errorCode === "string" ? data.errorCode.trim() : "";
				if (!code) {
					return null;
				}
				const heading = data.heading && typeof data.heading.msg === "string" ? data.heading.msg.trim() : "";
				const summary = data.summary && typeof data.summary.msg === "string" ? data.summary.msg.trim() : "";
				return { code, heading, summary };
			})();
			return { readyState: document.readyState, issue };
		})()`)
		if err != nil {
			return nil, err
		}
		m, ok := value.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unexpected page issue result type %T", value)
		}
		if rawIssue, ok := m["issue"]; ok && rawIssue != nil {
			issueMap, ok := rawIssue.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("unexpected page issue payload type %T", rawIssue)
			}
			issue := &pageIssue{
				Code:    strings.TrimSpace(fmt.Sprint(issueMap["code"])),
				Heading: strings.TrimSpace(fmt.Sprint(issueMap["heading"])),
				Summary: strings.TrimSpace(fmt.Sprint(issueMap["summary"])),
			}
			if issue.Code != "" && issue.Code != "<nil>" {
				if issue.Heading == "<nil>" {
					issue.Heading = ""
				}
				if issue.Summary == "<nil>" {
					issue.Summary = ""
				}
				return issue, nil
			}
		}
		readyState := strings.TrimSpace(fmt.Sprint(m["readyState"]))
		if readyState == "complete" || checkCtx.Err() != nil {
			return nil, checkCtx.Err()
		}
		select {
		case <-checkCtx.Done():
			return nil, checkCtx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func formatTargetTitle(target cdp.TargetInfo) string {
	title := strings.TrimSpace(target.Title)
	if title != "" {
		return title
	}
	return "<untitled>"
}

func formatPageIssueSuffix(issue *pageIssue) string {
	if issue == nil || issue.Code == "" {
		return ""
	}
	parts := []string{issue.Code}
	if issue.Heading != "" {
		parts = append(parts, issue.Heading)
	}
	if issue.Summary != "" {
		parts = append(parts, issue.Summary)
	}
	return " [" + strings.Join(parts, " | ") + "]"
}
