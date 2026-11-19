package e2e

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

const outputDir = "e2e/output"

// Result is the outcome of running one Journey.
type Result struct {
	ID    string
	OK    bool
	Known bool
	Lines []string
}

type stepKey struct {
	journey string
	step    string
}

var issueURL = regexp.MustCompile(`^https://github\.com/pserrano95/pinacoteca/issues/[1-9][0-9]*$`)

func knownFailures() (map[stepKey]knownFailure, error) {
	steps := make(map[stepKey]int)
	for _, j := range Journeys {
		for _, step := range j.Steps {
			steps[stepKey{j.ID, step.What}]++
		}
	}
	known := make(map[stepKey]knownFailure)
	for _, entry := range CONOCIDOS {
		key := stepKey{entry.Journey, entry.Step}
		switch {
		case !issueURL.MatchString(entry.Issue):
			return nil, fmt.Errorf("%q / %q needs an issue URL from this repository", entry.Journey, entry.Step)
		case strings.TrimSpace(entry.Reason) == "":
			return nil, fmt.Errorf("%q / %q needs a reason", entry.Journey, entry.Step)
		case steps[key] != 1:
			return nil, fmt.Errorf("%q / %q must name exactly one existing step", entry.Journey, entry.Step)
		case known[key].Issue != "":
			return nil, fmt.Errorf("%q / %q has a duplicate entry", entry.Journey, entry.Step)
		}
		known[key] = entry
	}
	return known, nil
}

// FindBrowser locates a Chromium or Chrome executable. If E2E_CHROMIUM is set,
// that exact path is required.
func FindBrowser() (string, error) {
	if p := os.Getenv("E2E_CHROMIUM"); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("E2E_CHROMIUM=%q: %w", p, err)
		}
		return p, nil
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	macPaths := []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
	}
	for _, p := range macPaths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no Chromium or Chrome browser found; install Chromium or set E2E_CHROMIUM")
}

// RunAll executes the selected journeys and returns true if none failed unexpectedly.
func RunAll(selected []Journey) bool {
	known, err := knownFailures()
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ CONOCIDOS: %v\n", err)
		return false
	}
	browserPath, err := FindBrowser()
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ browser startup failed: %v\n", err)
		return false
	}

	_ = os.RemoveAll(outputDir)

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(browserPath),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("lang", "en-US"),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	defer cancelAlloc()

	var green, knownCount int
	var failed []string
	for _, j := range selected {
		res := runOne(allocCtx, j, known)
		mark := "✓"
		switch {
		case !res.OK:
			mark = "✗"
			failed = append(failed, res.ID)
		case res.Known:
			mark = "⚠"
			knownCount++
		default:
			green++
		}
		fmt.Printf("%s %s — %s\n", mark, j.ID, j.What)
		if !res.OK || res.Known {
			for _, line := range res.Lines {
				fmt.Println(line)
			}
		}
	}

	fmt.Printf("\n%d green, %d known", green, knownCount)
	if len(failed) > 0 {
		fmt.Printf("; failed: %s\n", strings.Join(failed, ", "))
		return false
	}
	fmt.Println()
	return true
}

func runOne(allocCtx context.Context, j Journey, known map[stepKey]knownFailure) Result {
	h, err := StartHarness()
	if err != nil {
		return Result{ID: j.ID, OK: false, Lines: []string{fmt.Sprintf("  ✗ could not start harness: %v", err)}}
	}
	defer h.Close()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	var (
		noiseMu    sync.Mutex
		noise      []string
		activeStep int
	)
	type request struct {
		step       int
		path       string
		lastStatus int64
	}
	requests := make(map[network.RequestID]*request)
	baseParsed, _ := url.Parse(h.BaseURL)
	addNoise := func(step int, message string) {
		noise = append(noise, fmt.Sprintf("  ✗ step %d: %s", step, message))
	}
	recordHTTPFailure := func(req *request, status int64) {
		if status < 400 || status == req.lastStatus {
			return
		}
		req.lastStatus = status
		if req.step > 0 && status < 500 {
			expected := j.Steps[req.step-1].ExpectedHTTP
			if expected != nil && expected.Path == req.path && expected.Status == status {
				return
			}
		}
		addNoise(req.step, fmt.Sprintf("HTTP %d on %s", status, req.path))
	}

	if err := chromedp.Listen(ctx, func(ev any) {
		noiseMu.Lock()
		defer noiseMu.Unlock()

		switch e := ev.(type) {
		case *runtime.EventExceptionThrown:
			msg := e.ExceptionDetails.Text
			if e.ExceptionDetails.Exception != nil && e.ExceptionDetails.Exception.Description != "" {
				msg = e.ExceptionDetails.Exception.Description
			}
			addNoise(activeStep, fmt.Sprintf("JavaScript error: %s", firstLine(msg)))
		case *runtime.EventConsoleAPICalled:
			if e.Type == runtime.ConsoleAPICalledTypeError {
				var parts []string
				for _, arg := range e.Args {
					if arg.Value != nil {
						parts = append(parts, string(arg.Value))
					} else if arg.Description != "" {
						parts = append(parts, arg.Description)
					}
				}
				addNoise(activeStep, "console.error: "+strings.Join(parts, " "))
			}
		case *network.EventRequestWillBeSent:
			if e.Request != nil {
				if u, err := url.Parse(e.Request.URL); err == nil && sameOrigin(u, baseParsed) {
					// Redirects reuse the request ID. Keep the initiating step even
					// when the response arrives during a later action.
					req := requests[e.RequestID]
					if req == nil {
						req = &request{step: activeStep}
						requests[e.RequestID] = req
					}
					req.path = u.EscapedPath()
				}
			}
		case *network.EventResponseReceived:
			if e.Response != nil && e.Response.Status >= 400 {
				if u, err := url.Parse(e.Response.URL); err == nil && sameOrigin(u, baseParsed) {
					req := requests[e.RequestID]
					if req == nil {
						req = &request{path: u.EscapedPath()}
						requests[e.RequestID] = req
					}
					recordHTTPFailure(req, e.Response.Status)
				}
			}
		case *network.EventResponseReceivedExtraInfo:
			// Chrome can abort a stylesheet or image load after receiving
			// HTTP headers without emitting ResponseReceived.
			if req := requests[e.RequestID]; req != nil {
				recordHTTPFailure(req, e.StatusCode)
			}
		case *network.EventLoadingFailed:
			// Navigation may cancel in-flight resources from the previous page.
			if req, ok := requests[e.RequestID]; ok && !e.Canceled {
				addNoise(req.step, fmt.Sprintf("resource load failed on %s: %s", req.path, e.ErrorText))
			}
		case *fetch.EventRequestPaused:
			reqID := e.RequestID
			rawURL := ""
			if e.Request != nil {
				rawURL = e.Request.URL
			}
			allowed := isAllowedOrigin(rawURL, baseParsed)
			if !allowed {
				addNoise(activeStep, "blocked external request: "+rawURL)
			}
			go func() {
				if allowed {
					_, _ = chromedp.Call(ctx, fetch.ContinueRequest, fetch.ContinueRequestParams{
						RequestID: reqID,
					})
				} else {
					_, _ = chromedp.Call(ctx, fetch.FailRequest, fetch.FailRequestParams{
						RequestID:   reqID,
						ErrorReason: network.ErrorReasonBlockedByClient,
					})
				}
			}()
		}
	}); err != nil {
		return Result{ID: j.ID, OK: false, Lines: []string{fmt.Sprintf("  ✗ could not attach browser listener: %v", err)}}
	}

	w, ht := int64(1280), int64(800)
	if j.Viewport == ViewportMobile {
		w, ht = 390, 844
	}

	if _, err := chromedp.Call(ctx, runtime.Enable, cdp.Empty{}); err != nil {
		return Result{ID: j.ID, OK: false, Lines: []string{fmt.Sprintf("  ✗ could not enable runtime: %v", err)}}
	}
	if _, err := chromedp.Call(ctx, network.Enable, network.EnableParams{}); err != nil {
		return Result{ID: j.ID, OK: false, Lines: []string{fmt.Sprintf("  ✗ could not enable network: %v", err)}}
	}
	if _, err := chromedp.Call(ctx, fetch.Enable, fetch.EnableParams{
		Patterns: []*fetch.RequestPattern{{URLPattern: "*", RequestStage: fetch.RequestStageRequest}},
	}); err != nil {
		return Result{ID: j.ID, OK: false, Lines: []string{fmt.Sprintf("  ✗ could not enable fetch: %v", err)}}
	}
	if _, err := chromedp.Call(ctx, emulation.SetDeviceMetricsOverride, emulation.SetDeviceMetricsOverrideParams{
		Width:             w,
		Height:            ht,
		DeviceScaleFactor: 1,
		Mobile:            j.Viewport == ViewportMobile,
	}); err != nil {
		return Result{ID: j.ID, OK: false, Lines: []string{fmt.Sprintf("  ✗ could not set viewport: %v", err)}}
	}
	if _, err := chromedp.Call(ctx, network.SetCookie, network.SetCookieParams{
		Name:  "pinacoteca_session",
		Value: h.SessionCookie,
		URL:   h.BaseURL,
	}); err != nil {
		return Result{ID: j.ID, OK: false, Lines: []string{fmt.Sprintf("  ✗ could not set session cookie: %v", err)}}
	}

	var (
		lines    []string
		broken   bool
		hasKnown bool
	)

	if err := Navigate(ctx, h, j.Start); err != nil {
		broken = true
		lines = append(lines, fmt.Sprintf("  ✗ 0. Open %s", j.Start), "     "+err.Error())
	}

	for i, step := range j.Steps {
		if broken {
			lines = append(lines, fmt.Sprintf("  · %d. %s (not reached)", i+1, step.What))
			continue
		}
		noiseMu.Lock()
		activeStep = i + 1
		noiseMu.Unlock()

		err := step.Do(ctx, h)
		entry, listed := known[stepKey{j.ID, step.What}]
		switch {
		case listed && err != nil:
			hasKnown = true
			lines = append(lines, fmt.Sprintf("  ⚠ %d. %s", i+1, step.What),
				fmt.Sprintf("     ⚠ known: %s (%s)", entry.Reason, entry.Issue), "     "+err.Error())
		case listed:
			broken = true
			lines = append(lines, fmt.Sprintf("  ✗ %d. %s", i+1, step.What),
				"     no longer fails: remove it from CONOCIDOS")
		case err != nil:
			broken = true
			lines = append(lines, fmt.Sprintf("  ✗ %d. %s", i+1, step.What), "     "+err.Error())
		default:
			lines = append(lines, fmt.Sprintf("  ✓ %d. %s", i+1, step.What))
		}
	}

	noiseMu.Lock()
	lines = append(lines, noise...)
	hasNoise := len(noise) > 0
	noiseMu.Unlock()

	if broken || hasNoise {
		if shot, err := chromedp.Run(ctx, chromedp.FullScreenshot(100)); err == nil {
			if err := os.MkdirAll(outputDir, 0o755); err == nil {
				shotPath := filepath.Join(outputDir, j.ID+".png")
				if err := os.WriteFile(shotPath, shot, 0o644); err == nil {
					lines = append(lines, fmt.Sprintf("     screenshot: %s", shotPath))
				}
			}
		} else {
			lines = append(lines, "     (could not capture screenshot)")
		}
		return Result{ID: j.ID, OK: false, Lines: lines}
	}

	return Result{ID: j.ID, OK: true, Known: hasKnown, Lines: lines}
}

func isAllowedOrigin(rawURL string, base *url.URL) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "data", "blob", "about":
		return true
	}
	return sameOrigin(u, base)
}

func sameOrigin(u, base *url.URL) bool {
	return base != nil && u.Scheme == base.Scheme && u.Host == base.Host
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}
