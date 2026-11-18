package e2e

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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
	Lines []string
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

// RunAll executes the selected journeys and returns true if all passed.
func RunAll(selected []Journey) bool {
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

	results := make([]Result, 0, len(selected))
	for _, j := range selected {
		res := runOne(allocCtx, j)
		mark := "✓"
		if !res.OK {
			mark = "✗"
		}
		fmt.Printf("%s %s — %s\n", mark, j.ID, j.What)
		if !res.OK {
			for _, line := range res.Lines {
				fmt.Println(line)
			}
		}
		results = append(results, res)
	}

	var failed []string
	for _, r := range results {
		if !r.OK {
			failed = append(failed, r.ID)
		}
	}
	passed := len(results) - len(failed)
	if len(failed) > 0 {
		fmt.Printf("\n%d of %d journeys green; failed: %s\n", passed, len(results), strings.Join(failed, ", "))
		return false
	}
	fmt.Printf("\n%d of %d journeys green\n", passed, len(results))
	return true
}

func runOne(allocCtx context.Context, j Journey) Result {
	h, err := StartHarness()
	if err != nil {
		return Result{ID: j.ID, OK: false, Lines: []string{fmt.Sprintf("  ✗ could not start harness: %v", err)}}
	}
	defer h.Close()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	var (
		noiseMu sync.Mutex
		noise   []string
	)
	baseParsed, _ := url.Parse(h.BaseURL)
	if err := chromedp.Listen(ctx, func(ev any) {
		switch e := ev.(type) {
		case *runtime.EventExceptionThrown:
			msg := e.ExceptionDetails.Text
			if e.ExceptionDetails.Exception != nil && e.ExceptionDetails.Exception.Description != "" {
				msg = e.ExceptionDetails.Exception.Description
			}
			noiseMu.Lock()
			noise = append(noise, fmt.Sprintf("JavaScript error: %s", firstLine(msg)))
			noiseMu.Unlock()
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
				txt := strings.Join(parts, " ")
				if !strings.Contains(txt, "Failed to load resource") {
					noiseMu.Lock()
					noise = append(noise, fmt.Sprintf("console.error: %s", txt))
					noiseMu.Unlock()
				}
			}
		case *network.EventResponseReceived:
			if e.Response != nil && e.Response.Status >= 500 {
				if u, err := url.Parse(e.Response.URL); err == nil && baseParsed != nil && u.Host == baseParsed.Host {
					noiseMu.Lock()
					noise = append(noise, fmt.Sprintf("%d on %s", e.Response.Status, u.Path))
					noiseMu.Unlock()
				}
			}
		case *fetch.EventRequestPaused:
			reqID := e.RequestID
			rawURL := ""
			if e.Request != nil {
				rawURL = e.Request.URL
			}
			allowed := isAllowedOrigin(rawURL, baseParsed)
			if !allowed {
				noiseMu.Lock()
				noise = append(noise, fmt.Sprintf("blocked external request: %s", rawURL))
				noiseMu.Unlock()
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
		lines     []string
		broken    bool
		brokenErr error
	)

	if err := Navigate(ctx, h, j.Start); err != nil {
		broken = true
		brokenErr = err
		lines = append(lines, fmt.Sprintf("  ✗ 0. Open %s", j.Start))
	}

	for i, step := range j.Steps {
		if broken {
			lines = append(lines, fmt.Sprintf("  · %d. %s (not reached)", i+1, step.What))
			continue
		}
		if err := step.Do(ctx, h); err != nil {
			broken = true
			brokenErr = err
			lines = append(lines, fmt.Sprintf("  ✗ %d. %s", i+1, step.What))
		} else {
			lines = append(lines, fmt.Sprintf("  ✓ %d. %s", i+1, step.What))
		}
	}

	noiseMu.Lock()
	collectedNoise := append([]string(nil), noise...)
	noiseMu.Unlock()

	if broken || len(collectedNoise) > 0 {
		if brokenErr != nil {
			lines = append(lines, fmt.Sprintf("     %s", brokenErr.Error()))
		}
		for _, n := range collectedNoise {
			lines = append(lines, fmt.Sprintf("     %s", n))
		}
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

	return Result{ID: j.ID, OK: true, Lines: lines}
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
	return base != nil && u.Scheme == base.Scheme && u.Host == base.Host
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}
