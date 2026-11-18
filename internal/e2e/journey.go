// Package e2e runs browser journeys against a local Pinacoteca instance.
// Each journey represents a real user flow: where it starts, what it clicks or
// submits, and what the person ends up seeing on screen.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// Patience is how long a step waits for an element or condition to appear
// before considering it broken.
const Patience = 5 * time.Second

// Viewport selects the browser window size for a journey.
type Viewport int

const (
	ViewportDesktop Viewport = iota
	ViewportMobile
)

// Step is a single user action or assertion in a journey.
type Step struct {
	What string
	Do   func(ctx context.Context, h *Harness) error
}

// Journey is an end-to-end user flow in the gallery.
type Journey struct {
	ID       string
	What     string
	Start    string
	Viewport Viewport
	Steps    []Step
}

// Until polls cond until it returns true or Patience expires.
func Until(ctx context.Context, what string, cond func(context.Context) (bool, error)) error {
	deadline := time.Now().Add(Patience)
	var lastErr error
	for {
		ok, err := cond(ctx)
		if err == nil && ok {
			return nil
		}
		if err != nil {
			lastErr = err
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return fmt.Errorf("did not happen (%s): %w", what, lastErr)
			}
			return fmt.Errorf("did not happen: %s", what)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// See waits until at least one element matching selector is visible.
func See(ctx context.Context, selector string) error {
	return Until(ctx, fmt.Sprintf("see %q", selector), func(c context.Context) (bool, error) {
		js := fmt.Sprintf(`(() => {
			const els = document.querySelectorAll(%s);
			for (const el of els) {
				const st = getComputedStyle(el);
				if (st.display !== 'none' && st.visibility !== 'hidden' && el.getClientRects().length > 0) {
					return true;
				}
			}
			return false;
		})()`, jsQuote(selector))
		return chromedp.Run(c, chromedp.Evaluate[bool](js))
	})
}

// SeeText waits until a visible element matching selector contains substr.
func SeeText(ctx context.Context, selector, substr string) error {
	return Until(ctx, fmt.Sprintf("see %q in %q", substr, selector), func(c context.Context) (bool, error) {
		js := fmt.Sprintf(`(() => {
			const needle = %s;
			const els = document.querySelectorAll(%s);
			for (const el of els) {
				const st = getComputedStyle(el);
				if (st.display !== 'none' && st.visibility !== 'hidden' && el.getClientRects().length > 0) {
					if ((el.textContent || '').includes(needle)) return true;
				}
			}
			return false;
		})()`, jsQuote(substr), jsQuote(selector))
		return chromedp.Run(c, chromedp.Evaluate[bool](js))
	})
}

// NotSeeText waits until no visible element matching selector contains substr.
func NotSeeText(ctx context.Context, selector, substr string) error {
	return Until(ctx, fmt.Sprintf("not see %q in %q", substr, selector), func(c context.Context) (bool, error) {
		js := fmt.Sprintf(`(() => {
			const needle = %s;
			const els = document.querySelectorAll(%s);
			for (const el of els) {
				const st = getComputedStyle(el);
				if (st.display !== 'none' && st.visibility !== 'hidden' && el.getClientRects().length > 0) {
					if ((el.textContent || '').includes(needle)) return true;
				}
			}
			return false;
		})()`, jsQuote(substr), jsQuote(selector))
		found, err := chromedp.Run(c, chromedp.Evaluate[bool](js))
		if err != nil {
			return false, err
		}
		return !found, nil
	})
}

type clickCoords struct {
	OK     bool    `json:"ok"`
	Reason string  `json:"reason"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
}

// Click clicks an element like a person: it verifies the element is visible,
// enabled, and that its center is not covered by another layer before clicking.
func Click(ctx context.Context, selector string) error {
	var coords clickCoords
	err := Until(ctx, fmt.Sprintf("clickable %q", selector), func(c context.Context) (bool, error) {
		js := fmt.Sprintf(`(() => {
			const el = document.querySelector(%s);
			if (!el) return { ok: false, reason: 'element not found' };
			const st = getComputedStyle(el);
			if (st.display === 'none' || st.visibility === 'hidden' || st.pointerEvents === 'none') {
				return { ok: false, reason: 'element not interactive' };
			}
			if (el.disabled) return { ok: false, reason: 'element disabled' };
			el.scrollIntoView({ block: 'center', inline: 'center' });
			const r = el.getBoundingClientRect();
			if (r.width === 0 || r.height === 0) return { ok: false, reason: 'zero size' };
			const x = r.left + r.width / 2;
			const y = r.top + r.height / 2;
			const top = document.elementFromPoint(x, y);
			if (!top || (top !== el && !el.contains(top))) {
				return { ok: false, reason: 'covered by another element: ' + (top ? top.tagName : 'none') };
			}
			return { ok: true, x, y };
		})()`, jsQuote(selector))
		res, err := chromedp.Run(c, chromedp.Evaluate[clickCoords](js))
		if err != nil {
			return false, err
		}
		if !res.OK {
			return false, fmt.Errorf("%s", res.Reason)
		}
		coords = res
		return true, nil
	})
	if err != nil {
		return err
	}
	_, err = chromedp.Run(ctx, chromedp.MouseClickXY(coords.X, coords.Y))
	return err
}

// Fill sets the value of a form input matching selector.
func Fill(ctx context.Context, selector, value string) error {
	if err := See(ctx, selector); err != nil {
		return err
	}
	_, err := chromedp.Run(ctx, chromedp.SetValue(selector, value))
	return err
}

// Select chooses an option value in a <select> element matching selector.
func Select(ctx context.Context, selector, value string) error {
	if err := See(ctx, selector); err != nil {
		return err
	}
	_, err := chromedp.Run(ctx, chromedp.SetValue(selector, value))
	return err
}

// UploadFile attaches a local file path to a file input matching selector.
func UploadFile(ctx context.Context, selector, path string) error {
	if err := See(ctx, selector); err != nil {
		return err
	}
	_, err := chromedp.Run(ctx, chromedp.SetUploadFiles(selector, []string{path}))
	return err
}

// SetSession switches the active session cookie in the browser context.
func SetSession(ctx context.Context, h *Harness, token string) error {
	_, err := chromedp.Call(ctx, network.SetCookie, network.SetCookieParams{
		Name:  "pinacoteca_session",
		Value: token,
		URL:   h.BaseURL,
	})
	return err
}

// Navigate opens path relative to the harness base URL and waits for main to be visible.
func Navigate(ctx context.Context, h *Harness, path string) error {
	u := strings.TrimRight(h.BaseURL, "/") + path
	if _, err := chromedp.Run(ctx, chromedp.Navigate(u)); err != nil {
		return err
	}
	return See(ctx, "main")
}

type viewportDims struct {
	ScrollWidth int `json:"scrollWidth"`
	InnerWidth  int `json:"innerWidth"`
}

// NoOverflow checks that the page is not wider than maxWidth pixels.
func NoOverflow(ctx context.Context, screen string, maxWidth int) error {
	dims, err := chromedp.Run(ctx, chromedp.Evaluate[viewportDims](`({
		scrollWidth: Math.max(document.documentElement.scrollWidth, document.body ? document.body.scrollWidth : 0),
		innerWidth: window.innerWidth
	})`))
	if err != nil {
		return err
	}
	if dims.ScrollWidth > maxWidth || dims.InnerWidth > maxWidth {
		return fmt.Errorf("%q overflows: page is %d px wide (innerWidth %d) in a %d px viewport", screen, dims.ScrollWidth, dims.InnerWidth, maxWidth)
	}
	return nil
}

// AttrEquals waits until the attribute on selector equals want.
func AttrEquals(ctx context.Context, selector, attr, want string) error {
	return Until(ctx, fmt.Sprintf("%s[%s]=%q", selector, attr, want), func(c context.Context) (bool, error) {
		js := fmt.Sprintf(`(() => {
			const el = document.querySelector(%s);
			return el ? (el.getAttribute(%s) || '') : '';
		})()`, jsQuote(selector), jsQuote(attr))
		got, err := chromedp.Run(c, chromedp.Evaluate[string](js))
		if err != nil {
			return false, err
		}
		return got == want, nil
	})
}

// ImageLoaded waits until the first image matching selector has finished loading with naturalWidth > 0.
func ImageLoaded(ctx context.Context, selector string) error {
	return Until(ctx, fmt.Sprintf("image %q loaded", selector), func(c context.Context) (bool, error) {
		js := fmt.Sprintf(`(() => {
			const img = document.querySelector(%s);
			return !!(img && img.complete && img.naturalWidth > 0);
		})()`, jsQuote(selector))
		return chromedp.Run(c, chromedp.Evaluate[bool](js))
	})
}

func jsQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
