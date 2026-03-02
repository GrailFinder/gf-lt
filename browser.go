package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/playwright-community/playwright-go"

	"gf-lt/models"
)

var (
	browserLogger    *slog.Logger
	pw               *playwright.Playwright
	browser          playwright.Browser
	browserStarted   bool
	browserStartMu   sync.Mutex
	page             playwright.Page
	browserAvailable bool
)

func checkPlaywright() {
	var err error
	pw, err = playwright.Run()
	if err != nil {
		if browserLogger != nil {
			browserLogger.Warn("playwright not available", "error", err)
		}
		return
	}
	browserAvailable = true
	if browserLogger != nil {
		browserLogger.Info("playwright tools available")
	}
}

func pwStart(args map[string]string) []byte {
	browserStartMu.Lock()
	defer browserStartMu.Unlock()

	if browserStarted {
		return []byte(`{"error": "Browser already started"}`)
	}

	headless := true
	if cfg != nil && !cfg.PlaywrightHeadless {
		headless = false
	}

	var err error
	browser, err = pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(headless),
	})
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to launch browser: %s"}`, err.Error()))
	}

	page, err = browser.NewPage()
	if err != nil {
		browser.Close()
		return []byte(fmt.Sprintf(`{"error": "failed to create page: %s"}`, err.Error()))
	}

	browserStarted = true
	return []byte(`{"success": true, "message": "Browser started"}`)
}

func pwStop(args map[string]string) []byte {
	browserStartMu.Lock()
	defer browserStartMu.Unlock()

	if !browserStarted {
		return []byte(`{"success": true, "message": "Browser was not running"}`)
	}

	if page != nil {
		page.Close()
		page = nil
	}
	if browser != nil {
		browser.Close()
		browser = nil
	}

	browserStarted = false
	return []byte(`{"success": true, "message": "Browser stopped"}`)
}

func pwIsRunning(args map[string]string) []byte {
	if browserStarted {
		return []byte(`{"running": true, "message": "Browser is running"}`)
	}
	return []byte(`{"running": false, "message": "Browser is not running"}`)
}

func pwNavigate(args map[string]string) []byte {
	url, ok := args["url"]
	if !ok || url == "" {
		return []byte(`{"error": "url not provided"}`)
	}

	if !browserStarted || page == nil {
		return []byte(`{"error": "Browser not started. Call pw_start first."}`)
	}

	_, err := page.Goto(url)
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to navigate: %s"}`, err.Error()))
	}

	title, _ := page.Title()
	pageURL := page.URL()
	return []byte(fmt.Sprintf(`{"success": true, "title": "%s", "url": "%s"}`, title, pageURL))
}

func pwClick(args map[string]string) []byte {
	selector, ok := args["selector"]
	if !ok || selector == "" {
		return []byte(`{"error": "selector not provided"}`)
	}

	if !browserStarted || page == nil {
		return []byte(`{"error": "Browser not started. Call pw_start first."}`)
	}

	index := 0
	if args["index"] != "" {
		fmt.Sscanf(args["index"], "%d", &index)
	}

	locator := page.Locator(selector)
	count, err := locator.Count()
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to find elements: %s"}`, err.Error()))
	}

	if index >= count {
		return []byte(fmt.Sprintf(`{"error": "Element not found at index %d (found %d elements)"}`, index, count))
	}

	err = locator.Nth(index).Click()
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to click: %s"}`, err.Error()))
	}

	return []byte(`{"success": true, "message": "Clicked element"}`)
}

func pwFill(args map[string]string) []byte {
	selector, ok := args["selector"]
	if !ok || selector == "" {
		return []byte(`{"error": "selector not provided"}`)
	}

	text := args["text"]
	if text == "" {
		text = ""
	}

	if !browserStarted || page == nil {
		return []byte(`{"error": "Browser not started. Call pw_start first."}`)
	}

	index := 0
	if args["index"] != "" {
		fmt.Sscanf(args["index"], "%d", &index)
	}

	locator := page.Locator(selector)
	count, err := locator.Count()
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to find elements: %s"}`, err.Error()))
	}

	if index >= count {
		return []byte(fmt.Sprintf(`{"error": "Element not found at index %d"}`, index))
	}

	err = locator.Nth(index).Fill(text)
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to fill: %s"}`, err.Error()))
	}

	return []byte(`{"success": true, "message": "Filled input"}`)
}

func pwExtractText(args map[string]string) []byte {
	selector := args["selector"]
	if selector == "" {
		selector = "body"
	}

	if !browserStarted || page == nil {
		return []byte(`{"error": "Browser not started. Call pw_start first."}`)
	}

	locator := page.Locator(selector)
	count, err := locator.Count()
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to find elements: %s"}`, err.Error()))
	}

	if count == 0 {
		return []byte(`{"error": "No elements found"}`)
	}

	if selector == "body" {
		text, err := page.TextContent("body")
		if err != nil {
			return []byte(fmt.Sprintf(`{"error": "failed to get text: %s"}`, err.Error()))
		}
		return []byte(fmt.Sprintf(`{"text": "%s"}`, text))
	}

	var texts []string
	for i := 0; i < count; i++ {
		text, err := locator.Nth(i).TextContent()
		if err != nil {
			continue
		}
		texts = append(texts, text)
	}

	return []byte(fmt.Sprintf(`{"text": "%s"}`, joinLines(texts)))
}

func joinLines(lines []string) string {
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}

func pwScreenshot(args map[string]string) []byte {
	selector := args["selector"]
	fullPage := args["full_page"] == "true"

	if !browserStarted || page == nil {
		return []byte(`{"error": "Browser not started. Call pw_start first."}`)
	}

	path := fmt.Sprintf("/tmp/pw_screenshot_%d.png", os.Getpid())

	var err error
	if selector != "" && selector != "body" {
		locator := page.Locator(selector)
		_, err = locator.Screenshot(playwright.LocatorScreenshotOptions{
			Path: playwright.String(path),
		})
	} else {
		_, err = page.Screenshot(playwright.PageScreenshotOptions{
			Path:     playwright.String(path),
			FullPage: playwright.Bool(fullPage),
		})
	}

	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to take screenshot: %s"}`, err.Error()))
	}

	return []byte(fmt.Sprintf(`{"path": "%s"}`, path))
}

func pwScreenshotAndView(args map[string]string) []byte {
	selector := args["selector"]
	fullPage := args["full_page"] == "true"

	if !browserStarted || page == nil {
		return []byte(`{"error": "Browser not started. Call pw_start first."}`)
	}

	path := fmt.Sprintf("/tmp/pw_screenshot_%d.png", os.Getpid())

	var err error
	if selector != "" && selector != "body" {
		locator := page.Locator(selector)
		_, err = locator.Screenshot(playwright.LocatorScreenshotOptions{
			Path: playwright.String(path),
		})
	} else {
		_, err = page.Screenshot(playwright.PageScreenshotOptions{
			Path:     playwright.String(path),
			FullPage: playwright.Bool(fullPage),
		})
	}

	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to take screenshot: %s"}`, err.Error()))
	}

	dataURL, err := models.CreateImageURLFromPath(path)
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to create image URL: %s"}`, err.Error()))
	}

	resp := models.MultimodalToolResp{
		Type: "multimodal_content",
		Parts: []map[string]string{
			{"type": "text", "text": "Screenshot saved: " + path},
			{"type": "image_url", "url": dataURL},
		},
	}
	jsonResult, err := json.Marshal(resp)
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to marshal result: %s"}`, err.Error()))
	}
	return jsonResult
}

func pwWaitForSelector(args map[string]string) []byte {
	selector, ok := args["selector"]
	if !ok || selector == "" {
		return []byte(`{"error": "selector not provided"}`)
	}

	if !browserStarted || page == nil {
		return []byte(`{"error": "Browser not started. Call pw_start first."}`)
	}

	timeout := 30000
	if args["timeout"] != "" {
		fmt.Sscanf(args["timeout"], "%d", &timeout)
	}

	_, err := page.WaitForSelector(selector, playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(float64(timeout)),
	})
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "element not found: %s"}`, err.Error()))
	}

	return []byte(`{"success": true, "message": "Element found"}`)
}

func pwDrag(args map[string]string) []byte {
	x1, ok := args["x1"]
	if !ok {
		return []byte(`{"error": "x1 not provided"}`)
	}

	y1, ok := args["y1"]
	if !ok {
		return []byte(`{"error": "y1 not provided"}`)
	}

	x2, ok := args["x2"]
	if !ok {
		return []byte(`{"error": "x2 not provided"}`)
	}

	y2, ok := args["y2"]
	if !ok {
		return []byte(`{"error": "y2 not provided"}`)
	}

	if !browserStarted || page == nil {
		return []byte(`{"error": "Browser not started. Call pw_start first."}`)
	}

	var fx1, fy1, fx2, fy2 float64
	fmt.Sscanf(x1, "%f", &fx1)
	fmt.Sscanf(y1, "%f", &fy1)
	fmt.Sscanf(x2, "%f", &fx2)
	fmt.Sscanf(y2, "%f", &fy2)

	mouse := page.Mouse()

	err := mouse.Move(fx1, fy1)
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to move mouse: %s"}`, err.Error()))
	}

	err = mouse.Down()
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to mouse down: %s"}`, err.Error()))
	}

	err = mouse.Move(fx2, fy2)
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to move mouse: %s"}`, err.Error()))
	}

	err = mouse.Up()
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to mouse up: %s"}`, err.Error()))
	}

	return []byte(fmt.Sprintf(`{"success": true, "message": "Dragged from (%s,%s) to (%s,%s)"}`, x1, y1, x2, y2))
}

func init() {
	browserLogger = logger.With("component", "browser")
	checkPlaywright()
}
