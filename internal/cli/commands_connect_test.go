package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/veilm/cdp-cli/internal/cdp"
	"github.com/veilm/cdp-cli/internal/store"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

const connectTestURL = "https://example.test/index.php?page=post&s=list&tags=genshin_impact+official_art&encoded=%2B%26#results"

func TestConnectTargets(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		opened   string
		targetID string
		activate bool
	}{
		{"new positional", []string{"--new", connectTestURL}, connectTestURL, "full", true},
		{"new url", []string{"--new", "--url", connectTestURL}, connectTestURL, "full", true},
		{"legacy new-url", []string{"--new", "--new-url", connectTestURL}, connectTestURL, "full", true},
		{"blank", []string{"--new"}, "about:blank", "full", true},
		{"explicit blank", []string{"--new", "--new-url", "about:blank"}, "about:blank", "full", true},
		{"equals url", []string{"--new", "--url=" + connectTestURL}, connectTestURL, "full", true},
		{"equals new-url", []string{"--new", "--new-url=" + connectTestURL}, connectTestURL, "full", true},
		{"interspersed flags", []string{connectTestURL, "--activate=false", "--new"}, connectTestURL, "full", false},
		{"url before new", []string{"--url", connectTestURL, "--new"}, connectTestURL, "full", true},
		{"end of flags", []string{"--new", "--", connectTestURL}, connectTestURL, "full", true},
		{"identical aliases", []string{"--new", connectTestURL, "--url", connectTestURL, "--new-url", connectTestURL}, connectTestURL, "full", true},
		{"identical repeated url", []string{"--new", "--url", connectTestURL, "--url", connectTestURL}, connectTestURL, "full", true},
		{"identical repeated new-url", []string{"--new", "--new-url", connectTestURL, "--new-url", connectTestURL}, connectTestURL, "full", true},
		{"legacy existing url", []string{"--url", connectTestURL}, "", "full", false},
		{"existing positional", []string{connectTestURL}, "", "full", false},
		{"existing identical aliases", []string{connectTestURL, "--url", connectTestURL}, "", "full", false},
		{"existing case insensitive exact", []string{strings.ToUpper(connectTestURL)}, "", "full", false},
		{"existing first prefix", []string{"https://example.test"}, "", "short", false},
		{"existing reverse prefix", []string{connectTestURL + "extra"}, "", "short", false},
		{"existing substring", []string{"genshin_impact+official_art"}, "", "full", false},
		{"legacy tab index", []string{"--tab", "2"}, "", "full", false},
		{"legacy tab id", []string{"--tab", "full"}, "", "full", false},
		{"legacy tab pattern", []string{"--tab", "official_art"}, "", "full", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			pageURL := connectTestURL
			if tt.opened != "" {
				pageURL = tt.opened
			}
			browser := newConnectTestBrowser(t, pageURL)
			args := append([]string{"--session", "test-connect", "--port", browser.port}, tt.args...)
			if _, err := captureStdout(t, func() error { return cmdConnect(args) }); err != nil {
				t.Fatal(err)
			}
			browser.mu.Lock()
			defer browser.mu.Unlock()
			if tt.opened == "" {
				if len(browser.opened) != 0 {
					t.Fatalf("existing-tab connect created tabs: %q", browser.opened)
				}
			} else if len(browser.opened) != 1 || browser.opened[0] != tt.opened {
				t.Fatalf("opened %q, want exactly [%q]", browser.opened, tt.opened)
			}
			if tt.activate && (len(browser.activated) != 1 || browser.activated[0] != tt.targetID) {
				t.Fatalf("activated %q, want [%q]", browser.activated, tt.targetID)
			}
			if !tt.activate && len(browser.activated) != 0 {
				t.Fatalf("unexpected activation: %q", browser.activated)
			}
			st, err := store.Load()
			if err != nil {
				t.Fatal(err)
			}
			session, ok := st.Get("test-connect")
			wantURL := pageURL
			if tt.targetID == "short" {
				wantURL = "https://example.test/index.php"
			}
			if !ok || session.TargetID != tt.targetID || session.URL != wantURL {
				t.Fatalf("saved session %+v, want target %q and URL %q", session, tt.targetID, wantURL)
			}
		})
	}
}

func TestConnectRejectsAmbiguousTargetsBeforeBrowserAccess(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"new tab", []string{"--new", "--tab", "full"}, "--new cannot be combined with --tab"},
		{"new tab and url", []string{"--new", "--tab", "full", connectTestURL}, "--new cannot be combined with --tab"},
		{"new positional and url", []string{"--new", connectTestURL, "--url", "https://other.test"}, "conflicting URLs"},
		{"new positional and new-url", []string{"--new", connectTestURL, "--new-url", "about:blank"}, "conflicting URLs"},
		{"new url and new-url", []string{"--new", "--url", connectTestURL, "--new-url", "https://other.test"}, "conflicting URLs"},
		{"repeated url", []string{"--new", "--url", connectTestURL, "--url", "https://other.test"}, "conflicting URLs"},
		{"repeated new-url", []string{"--new", "--new-url", connectTestURL, "--new-url", "https://other.test"}, "conflicting URLs"},
		{"existing positional and url", []string{connectTestURL, "--url", "https://other.test"}, "conflicting URLs"},
		{"existing repeated url", []string{"--url", connectTestURL, "--url", "https://other.test"}, "conflicting URLs"},
		{"two positionals", []string{"--new", connectTestURL, "https://other.test"}, "one positional URL"},
		{"existing tab and positional", []string{"--tab", "full", connectTestURL}, "--tab cannot be combined with a URL"},
		{"existing tab and url", []string{"--tab", "full", "--url", connectTestURL}, "--tab cannot be combined with a URL"},
		{"new-url requires new", []string{"--new-url", connectTestURL}, "--new-url requires --new"},
		{"no ignored new-url", []string{"--url", connectTestURL, "--new-url", connectTestURL}, "--new-url requires --new"},
		{"no target", nil, "a URL, --tab"},
		{"empty positional", []string{"--new", ""}, "positional URL must not be empty"},
		{"empty url", []string{"--new", "--url="}, "--url must not be empty"},
		{"empty new-url", []string{"--new", "--new-url="}, "--new-url must not be empty"},
		{"unknown option", []string{"--nw"}, "unexpected argument: --nw"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Keep the user's session file isolated even if validation regresses.
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			browser := newConnectTestBrowser(t, connectTestURL)
			args := append([]string{"--session", "test-connect", "--port", browser.port}, tt.args...)
			err := cmdConnect(args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want error containing %q", err, tt.want)
			}
			browser.mu.Lock()
			defer browser.mu.Unlock()
			if browser.requests != 0 {
				t.Fatalf("invalid arguments sent %d browser requests", browser.requests)
			}
		})
	}
}

func TestConnectHelp(t *testing.T) {
	output, err := captureStdout(t, func() error { return cmdConnect([]string{"--help"}) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--new [<url>]", "--url and --new-url are aliases", "matches an existing tab", "Different URLs conflict", "--tab cannot be combined"} {
		if !strings.Contains(output, want) {
			t.Errorf("help missing %q: %s", want, output)
		}
	}
}

type connectTestBrowser struct {
	port      string
	mu        sync.Mutex
	requests  int
	opened    []string
	activated []string
}

// Serve enough CDP to exercise target creation, matching, handshakes and saved
// sessions, while keeping all tests independent of real browsers.
func newConnectTestBrowser(t *testing.T, pageURL string) *connectTestBrowser {
	t.Helper()
	browser := &connectTestBrowser{}
	var targets []cdp.TargetInfo
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		browser.mu.Lock()
		browser.requests++
		browser.mu.Unlock()
		switch {
		case r.URL.Path == "/json/new":
			if r.Method != http.MethodPut {
				t.Errorf("create method = %s, want PUT", r.Method)
			}
			opened, err := url.QueryUnescape(r.URL.RawQuery)
			if err != nil {
				t.Error(err)
			}
			browser.mu.Lock()
			browser.opened = append(browser.opened, opened)
			browser.mu.Unlock()
			_ = json.NewEncoder(w).Encode(targets[1])
		case r.URL.Path == "/json/list":
			_ = json.NewEncoder(w).Encode(targets)
		case strings.HasPrefix(r.URL.Path, "/json/activate/"):
			browser.mu.Lock()
			browser.activated = append(browser.activated, strings.TrimPrefix(r.URL.Path, "/json/activate/"))
			browser.mu.Unlock()
		case strings.HasPrefix(r.URL.Path, "/devtools/page/"):
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.CloseNow()
			for {
				var request struct {
					ID     int `json:"id"`
					Params struct {
						Expression string `json:"expression"`
					} `json:"params"`
				}
				if err := wsjson.Read(context.Background(), conn, &request); err != nil {
					return
				}
				var value interface{} = "complete"
				if request.Params.Expression != "document.readyState" {
					value = map[string]interface{}{"readyState": "complete", "issue": nil}
				}
				response := map[string]interface{}{"id": request.ID, "result": map[string]interface{}{
					"result": map[string]interface{}{"type": "object", "value": value},
				}}
				if err := wsjson.Write(context.Background(), conn, response); err != nil {
					return
				}
			}
		default:
			t.Errorf("unexpected browser request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	browser.port = parsedURL.Port()
	for _, target := range []struct{ id, url string }{{"short", "https://example.test/index.php"}, {"full", pageURL}} {
		targets = append(targets, cdp.TargetInfo{
			ID: target.id, Title: target.id, Type: "page", URL: target.url,
			WebSocket: "ws://" + parsedURL.Host + "/devtools/page/" + target.id,
		})
	}
	return browser
}
