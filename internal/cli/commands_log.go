package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/veilm/cdp-cli/internal/cdp"
	"github.com/veilm/cdp-cli/internal/format"
	"github.com/veilm/cdp-cli/internal/store"
)

func cmdLog(args []string) error {
	fs := newFlagSet("log", "usage: cdp log <name> [\"setup script\"] [options]")
	limitFlag := fs.Int("limit", 0, "Maximum log entries to collect (<=0 for unlimited)")
	timeoutFlag := fs.Duration("timeout", 0, "Maximum time to wait for log events (0 disables)")
	levelFlag := fs.String("level", "", "Regex to filter by level/type (e.g. 'error|warning|exception')")
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 1 {
		fs.Usage()
		return errors.New("usage: cdp log <name> [\"setup script\"] [options]")
	}
	name := pos[0]
	script := ""
	if len(pos) > 1 {
		script = pos[1]
	}
	if len(pos) > 2 {
		return fmt.Errorf("unexpected argument: %s", pos[2])
	}
	limit := *limitFlag
	timeout := *timeoutFlag

	var levelFilter *regexp.Regexp
	if *levelFlag != "" {
		levelSpec := escapeLeadingPlusRegexSpec(*levelFlag)
		levelFilter, err = regexp.Compile(levelSpec)
		if err != nil {
			return fmt.Errorf("invalid --level regex: %w", err)
		}
	}

	st, err := store.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle, err := openSession(ctx, st, name)
	if err != nil {
		return err
	}
	defer handle.Close()

	if err := handle.client.Call(ctx, "Runtime.enable", nil, nil); err != nil {
		return err
	}
	if err := handle.client.Call(ctx, "Log.enable", nil, nil); err != nil {
		return err
	}

	events := make(chan cdp.Event, 64)
	unsubscribe := handle.client.SubscribeEvents(func(evt cdp.Event) {
		select {
		case events <- evt:
		default:
		}
	})
	defer unsubscribe()

	if script != "" {
		if _, err := handle.client.Evaluate(ctx, script); err != nil {
			return fmt.Errorf("setup script failed: %w", err)
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	var timer *time.Timer
	var timeoutCh <-chan time.Time
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		timeoutCh = timer.C
		defer timer.Stop()
	}

	limitInfo := "unlimited"
	if limit > 0 {
		limitInfo = strconv.Itoa(limit)
	}
	timeoutInfo := "none"
	if timeout > 0 {
		timeoutInfo = timeout.String()
	}
	fmt.Fprintf(os.Stderr, "Streaming console output (limit=%s, timeout=%s). Ctrl+C to stop.\n", limitInfo, timeoutInfo)

	logCount := 0
	exitReason := ""

loop:
	for {
		switch {
		case ctx.Err() != nil:
			if exitReason == "" {
				exitReason = "context cancelled"
			}
			break loop
		case timeoutCh != nil:
			select {
			case <-timeoutCh:
				exitReason = fmt.Sprintf("timeout reached (%s)", timeout)
				break loop
			default:
			}
		}
		select {
		case <-ctx.Done():
			if exitReason == "" {
				exitReason = "context cancelled"
			}
			break loop
		case evt := <-events:
			printed, err := handleLogEvent(ctx, handle.client, evt, levelFilter)
			if err != nil {
				fmt.Fprintln(os.Stderr, "log handler:", err)
			}
			if printed {
				logCount++
			}
			if limit > 0 && logCount >= limit {
				exitReason = fmt.Sprintf("limit reached (%d entries)", limit)
				break loop
			}
		case <-timeoutCh:
			exitReason = fmt.Sprintf("timeout reached (%s)", timeout)
			break loop
		case <-sigCh:
			exitReason = "interrupted"
			cancel()
			break loop
		}
	}

	if exitReason == "" {
		exitReason = "completed"
	}
	fmt.Fprintf(os.Stderr, "Log stream ended (%s). Entries: %d\n", exitReason, logCount)
	return nil
}

func handleLogEvent(ctx context.Context, client *cdp.Client, evt cdp.Event, levelFilter *regexp.Regexp) (bool, error) {
	switch evt.Method {
	case "Runtime.consoleAPICalled":
		var payload struct {
			Type string             `json:"type"`
			Args []cdp.RemoteObject `json:"args"`
		}
		if err := json.Unmarshal(evt.Params, &payload); err != nil {
			return false, err
		}
		if levelFilter != nil && !levelFilter.MatchString(payload.Type) {
			return false, nil
		}
		values := make([]string, 0, len(payload.Args))
		for _, arg := range payload.Args {
			val, err := client.RemoteObjectValue(ctx, arg)
			if err != nil {
				values = append(values, fmt.Sprintf("<error: %v>", err))
				continue
			}
			switch t := val.(type) {
			case string:
				values = append(values, t)
			default:
				out, err := format.JSON(t, false, 2)
				if err != nil {
					values = append(values, fmt.Sprintf("%v", t))
				} else {
					values = append(values, out)
				}
			}
		}
		fmt.Printf("[%s] %s\n", payload.Type, strings.Join(values, " "))
		return true, nil

	case "Runtime.exceptionThrown":
		if levelFilter != nil && !levelFilter.MatchString("exception") {
			return false, nil
		}
		var payload struct {
			ExceptionDetails struct {
				Text      string `json:"text"`
				Exception *struct {
					Description string           `json:"description"`
					Value       *json.RawMessage `json:"value"`
				} `json:"exception"`
				StackTrace *struct {
					CallFrames []struct {
						FunctionName string `json:"functionName"`
						URL          string `json:"url"`
						LineNumber   int    `json:"lineNumber"`
						ColumnNumber int    `json:"columnNumber"`
					} `json:"callFrames"`
				} `json:"stackTrace"`
			} `json:"exceptionDetails"`
		}
		if err := json.Unmarshal(evt.Params, &payload); err != nil {
			return false, err
		}
		details := payload.ExceptionDetails
		desc := ""
		if details.Exception != nil {
			desc = details.Exception.Description
			if desc == "" && details.Exception.Value != nil {
				desc = string(*details.Exception.Value)
			}
		}
		if desc != "" {
			fmt.Printf("[exception] %s\n", desc)
		} else {
			fmt.Printf("[exception] %s\n", details.Text)
			if details.StackTrace != nil {
				for _, f := range details.StackTrace.CallFrames {
					fn := f.FunctionName
					if fn == "" {
						fn = "(anonymous)"
					}
					fmt.Printf("  at %s (%s:%d:%d)\n", fn, f.URL, f.LineNumber+1, f.ColumnNumber+1)
				}
			}
		}
		return true, nil

	case "Log.entryAdded":
		var payload struct {
			Entry struct {
				Source string `json:"source"`
				Level  string `json:"level"`
				Text   string `json:"text"`
				URL    string `json:"url"`
				Line   int    `json:"lineNumber"`
				Column int    `json:"columnNumber"`
			} `json:"entry"`
		}
		if err := json.Unmarshal(evt.Params, &payload); err != nil {
			return false, err
		}
		entry := payload.Entry
		if levelFilter != nil && !levelFilter.MatchString(entry.Level) {
			return false, nil
		}
		location := ""
		if entry.URL != "" {
			location = fmt.Sprintf(" (%s:%d:%d)", entry.URL, entry.Line, entry.Column)
		}
		fmt.Printf("[%s/%s] %s%s\n", entry.Source, entry.Level, entry.Text, location)
		return true, nil
	}
	return false, nil
}

func cmdNetworkLog(args []string) error {
	fs := newFlagSet("network-log", "usage: cdp network-log <name> [options]")
	dirFlag := fs.String("dir", "", "Directory for captured requests (default ./cdp-<name>-network-log)")
	urlPattern := fs.String("url", "", "Regex to match request URLs")
	methodPattern := fs.String("method", "", "Regex to match HTTP methods")
	statusPattern := fs.String("status", "", "Regex to match HTTP status codes")
	mimePattern := fs.String("mime", "", "Regex to match response Content-Type values")
	if len(args) == 0 {
		fs.Usage()
		return errors.New("usage: cdp network-log <name> [options]")
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
		return errors.New("usage: cdp network-log <name> [options]")
	}
	name := pos[0]
	if len(pos) > 1 {
		return fmt.Errorf("unexpected argument: %s", pos[1])
	}

	filters, err := buildNetworkFilters(*urlPattern, *methodPattern, *statusPattern, *mimePattern)
	if err != nil {
		return err
	}

	outputDir := *dirFlag
	if outputDir == "" {
		sessionFragment := sanitizePathFragment(name)
		if sessionFragment == "" {
			sessionFragment = "session"
		}
		outputDir = fmt.Sprintf("cdp-%s-network-log", sessionFragment)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	st, err := store.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle, err := openSession(ctx, st, name)
	if err != nil {
		return err
	}
	defer handle.Close()

	opts := networkCaptureOptions{
		Dir:     outputDir,
		Filters: filters,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- runNetworkCapture(ctx, handle.client, opts)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case <-sigCh:
		cancel()
		err := <-errCh
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	case err := <-errCh:
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
}

// network-log helpers

type networkCaptureOptions struct {
	Dir     string
	Filters networkFilters
}

type networkFilters struct {
	url    *regexp.Regexp
	method *regexp.Regexp
	status *regexp.Regexp
	mime   *regexp.Regexp
}

func buildNetworkFilters(urlPattern, methodPattern, statusPattern, mimePattern string) (networkFilters, error) {
	var filters networkFilters
	var err error
	if urlPattern != "" {
		urlPattern = escapeLeadingPlusRegexSpec(urlPattern)
		filters.url, err = regexp.Compile(urlPattern)
		if err != nil {
			return filters, fmt.Errorf("invalid --url regex: %w", err)
		}
	}
	if methodPattern != "" {
		methodPattern = escapeLeadingPlusRegexSpec(methodPattern)
		filters.method, err = regexp.Compile(methodPattern)
		if err != nil {
			return filters, fmt.Errorf("invalid --method regex: %w", err)
		}
	}
	if statusPattern != "" {
		statusPattern = escapeLeadingPlusRegexSpec(statusPattern)
		filters.status, err = regexp.Compile(statusPattern)
		if err != nil {
			return filters, fmt.Errorf("invalid --status regex: %w", err)
		}
	}
	if mimePattern != "" {
		mimePattern = escapeLeadingPlusRegexSpec(mimePattern)
		filters.mime, err = regexp.Compile(mimePattern)
		if err != nil {
			return filters, fmt.Errorf("invalid --mime regex: %w", err)
		}
	}
	return filters, nil
}

func (f networkFilters) match(url, method, status, mime string) bool {
	if f.url != nil && !f.url.MatchString(url) {
		return false
	}
	if f.method != nil && !f.method.MatchString(method) {
		return false
	}
	if f.status != nil && !f.status.MatchString(status) {
		return false
	}
	if f.mime != nil && !f.mime.MatchString(mime) {
		return false
	}
	return true
}

type fetchRequestPausedEvent struct {
	RequestID          string             `json:"requestId"`
	Request            fetchRequestInfo   `json:"request"`
	ResponseStatusCode *int               `json:"responseStatusCode"`
	ResponseHeaders    []fetchHeaderEntry `json:"responseHeaders"`
	RequestStage       string             `json:"requestStage"`
}

type fetchRequestInfo struct {
	URL      string                 `json:"url"`
	Method   string                 `json:"method"`
	Headers  map[string]interface{} `json:"headers"`
	PostData string                 `json:"postData"`
}

type fetchHeaderEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func runNetworkCapture(ctx context.Context, client *cdp.Client, opts networkCaptureOptions) error {
	if err := client.Call(ctx, "Network.enable", nil, nil); err != nil {
		return err
	}
	if err := client.Call(ctx, "Fetch.enable", map[string]interface{}{
		"patterns": []map[string]interface{}{
			{
				"urlPattern":   "*",
				"requestStage": "Response",
			},
		},
		"handleAuthRequests": false,
	}, nil); err != nil {
		return err
	}
	defer func() {
		disableCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		client.Call(disableCtx, "Fetch.disable", nil, nil)
	}()

	var wg sync.WaitGroup
	unsubscribe := client.SubscribeEvents(func(evt cdp.Event) {
		if evt.Method != "Fetch.requestPaused" {
			return
		}
		var payload fetchRequestPausedEvent
		if err := json.Unmarshal(evt.Params, &payload); err != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
		wg.Add(1)
		go func(event fetchRequestPausedEvent) {
			defer wg.Done()
			processFetchPaused(ctx, client, opts, event)
		}(payload)
	})
	defer func() {
		unsubscribe()
		wg.Wait()
	}()

	<-ctx.Done()
	return ctx.Err()
}

type networkCapture struct {
	Timestamp         time.Time
	RequestID         string
	URL               string
	Method            string
	Stage             string
	Status            string
	ContentType       string
	RequestHeaders    map[string]string
	ResponseHeaders   map[string]string
	RequestBody       []byte
	ResponseBody      []byte
	ResponseBodyError string
}

func processFetchPaused(ctx context.Context, client *cdp.Client, opts networkCaptureOptions, event fetchRequestPausedEvent) {
	defer continueFetchRequest(client, event.RequestID)

	url := event.Request.URL
	method := event.Request.Method
	status := "<pending>"
	if event.ResponseStatusCode != nil {
		status = strconv.Itoa(*event.ResponseStatusCode)
	}
	responseHeaders := normalizeHeaderList(event.ResponseHeaders)
	contentType := strings.ToLower(responseHeaders["content-type"])
	if !opts.Filters.match(url, method, status, contentType) {
		return
	}

	body, bodyErr := fetchResponseBody(ctx, client, event.RequestID)
	requestHeaders := sanitizeHeaderMap(event.Request.Headers)
	var requestBody []byte
	if event.Request.PostData != "" {
		requestBody = []byte(event.Request.PostData)
	}

	capture := networkCapture{
		Timestamp:         time.Now(),
		RequestID:         event.RequestID,
		URL:               url,
		Method:            method,
		Stage:             event.RequestStage,
		Status:            status,
		ContentType:       contentType,
		RequestHeaders:    requestHeaders,
		ResponseHeaders:   responseHeaders,
		RequestBody:       requestBody,
		ResponseBody:      body,
		ResponseBodyError: bodyErr,
	}
	if err := writeNetworkCapture(opts.Dir, capture); err != nil {
		fmt.Fprintf(os.Stderr, "cdp network-log: failed to write capture for %s: %v\n", event.RequestID, err)
	}
}

func fetchResponseBody(ctx context.Context, client *cdp.Client, requestID string) ([]byte, string) {
	var result struct {
		Body          string `json:"body"`
		Base64Encoded bool   `json:"base64Encoded"`
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := client.Call(callCtx, "Fetch.getResponseBody", map[string]interface{}{
		"requestId": requestID,
	}, &result); err != nil {
		return nil, err.Error()
	}
	if result.Body == "" {
		return nil, ""
	}
	if result.Base64Encoded {
		data, err := base64.StdEncoding.DecodeString(result.Body)
		if err != nil {
			return nil, fmt.Sprintf("decode body: %v", err)
		}
		return data, ""
	}
	return []byte(result.Body), ""
}

func continueFetchRequest(client *cdp.Client, requestID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client.Call(ctx, "Fetch.continueRequest", map[string]interface{}{
		"requestId": requestID,
	}, nil)
}

func normalizeHeaderList(headers []fetchHeaderEntry) map[string]string {
	result := make(map[string]string, len(headers))
	for _, header := range headers {
		name := strings.ToLower(strings.TrimSpace(header.Name))
		if name == "" {
			continue
		}
		result[name] = header.Value
	}
	return result
}

func sanitizeHeaderMap(headers map[string]interface{}) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}
	result := make(map[string]string, len(headers))
	for key, value := range headers {
		if key == "" || value == nil {
			continue
		}
		result[key] = fmt.Sprint(value)
	}
	return result
}

func writeNetworkCapture(baseDir string, capture networkCapture) error {
	dirName := formatCaptureDirName(capture)
	captureDir := filepath.Join(baseDir, dirName)
	if err := os.MkdirAll(captureDir, 0o755); err != nil {
		return err
	}

	metadata := map[string]interface{}{
		"timestamp": capture.Timestamp.Format(time.RFC3339Nano),
		"requestId": capture.RequestID,
		"url":       capture.URL,
		"method":    capture.Method,
		"stage":     capture.Stage,
		"status":    capture.Status,
	}
	if capture.ContentType != "" {
		metadata["contentType"] = capture.ContentType
	}
	if capture.ResponseBodyError != "" {
		metadata["responseBodyError"] = capture.ResponseBodyError
	}
	if err := writeJSONFile(filepath.Join(captureDir, "metadata.json"), metadata); err != nil {
		return err
	}

	reqHeaders := capture.RequestHeaders
	if reqHeaders == nil {
		reqHeaders = map[string]string{}
	}
	if err := writeJSONFile(filepath.Join(captureDir, "request-headers.json"), reqHeaders); err != nil {
		return err
	}

	respHeaders := capture.ResponseHeaders
	if respHeaders == nil {
		respHeaders = map[string]string{}
	}
	if err := writeJSONFile(filepath.Join(captureDir, "response-headers.json"), respHeaders); err != nil {
		return err
	}

	if len(capture.RequestBody) > 0 {
		if err := os.WriteFile(filepath.Join(captureDir, "request-body.bin"), capture.RequestBody, 0o644); err != nil {
			return err
		}
	}
	if len(capture.ResponseBody) > 0 {
		if err := os.WriteFile(filepath.Join(captureDir, "response-body.bin"), capture.ResponseBody, 0o644); err != nil {
			return err
		}
		if err := writeResponseBodyJSON(filepath.Join(captureDir, "response-body.json"), capture.ResponseBody); err != nil {
			return err
		}
	}
	return nil
}

func sanitizePathFragment(value string) string {
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func formatCaptureDirName(capture networkCapture) string {
	ms := capture.Timestamp.UnixNano() / int64(time.Millisecond)
	method := strings.ToUpper(strings.TrimSpace(capture.Method))
	if method == "" {
		method = "REQ"
	}
	urlFragment := shortenURLFragment(capture.URL, 96)
	return fmt.Sprintf("%d-%s-%s", ms, method, urlFragment)
}

func shortenURLFragment(raw string, limit int) string {
	fragment := normalizeURLFragment(raw)
	if limit <= 0 || len(fragment) <= limit {
		return fragment
	}
	if limit <= 6 {
		return fragment[:limit]
	}
	head := (limit - 3) / 2
	tail := limit - 3 - head
	return fragment[:head] + "..." + fragment[len(fragment)-tail:]
}

func normalizeURLFragment(raw string) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "url"
	}
	if u, err := url.Parse(clean); err == nil && u.Host != "" {
		clean = u.Host + u.Path
	} else {
		if i := strings.Index(clean, "://"); i != -1 {
			clean = clean[i+3:]
		}
		if i := strings.IndexAny(clean, "?#"); i != -1 {
			clean = clean[:i]
		}
	}
	clean = strings.SplitN(clean, "?", 2)[0]
	clean = strings.SplitN(clean, "#", 2)[0]
	clean = strings.Trim(clean, "/")
	clean = strings.ReplaceAll(clean, "/", "-")
	clean = strings.ToLower(clean)

	var b strings.Builder
	for _, r := range clean {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	fragment := strings.Trim(b.String(), "-_.")
	if fragment == "" {
		return "url"
	}
	return fragment
}
