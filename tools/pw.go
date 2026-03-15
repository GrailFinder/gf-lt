package tools

import (
	"encoding/json"
	"fmt"
	"gf-lt/models"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/playwright-community/playwright-go"
)

var (
	pw             *playwright.Playwright
	browser        playwright.Browser
	browserStarted bool
	browserStartMu sync.Mutex
	page           playwright.Page
)

func PwShutDown() error {
	if pw == nil {
		return nil
	}
	pwStop(nil)
	return pw.Stop()
}

func InstallPW() error {
	err := playwright.Install(&playwright.RunOptions{Verbose: false})
	if err != nil {
		logger.Warn("playwright not available", "error", err)
		return err
	}
	return nil
}

func CheckPlaywright() error {
	var err error
	pw, err = playwright.Run()
	if err != nil {
		logger.Warn("playwright not available", "error", err)
		return err
	}
	return nil
}

func pwStart(args map[string]string) []byte {
	browserStartMu.Lock()
	defer browserStartMu.Unlock()
	if browserStarted {
		return []byte(`{"error": "Browser already started"}`)
	}
	var err error
	browser, err = pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(!cfg.PlaywrightDebug),
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
		if i, err := strconv.Atoi(args["index"]); err != nil {
			logger.Warn("failed to parse index", "value", args["index"], "error", err)
		} else {
			index = i
		}
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
		if i, err := strconv.Atoi(args["index"]); err != nil {
			logger.Warn("failed to parse index", "value", args["index"], "error", err)
		} else {
			index = i
		}
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
		text, err := page.Locator("body").TextContent()
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
	var sb strings.Builder
	for i, line := range lines {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(line)
	}
	return sb.String()
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
		if t, err := strconv.Atoi(args["timeout"]); err != nil {
			logger.Warn("failed to parse timeout", "value", args["timeout"], "error", err)
		} else {
			timeout = t
		}
	}
	locator := page.Locator(selector)
	err := locator.WaitFor(playwright.LocatorWaitForOptions{
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
	if parsedX1, err := strconv.ParseFloat(x1, 64); err != nil {
		logger.Warn("failed to parse x1", "value", x1, "error", err)
	} else {
		fx1 = parsedX1
	}
	if parsedY1, err := strconv.ParseFloat(y1, 64); err != nil {
		logger.Warn("failed to parse y1", "value", y1, "error", err)
	} else {
		fy1 = parsedY1
	}
	if parsedX2, err := strconv.ParseFloat(x2, 64); err != nil {
		logger.Warn("failed to parse x2", "value", x2, "error", err)
	} else {
		fx2 = parsedX2
	}
	if parsedY2, err := strconv.ParseFloat(y2, 64); err != nil {
		logger.Warn("failed to parse y2", "value", y2, "error", err)
	} else {
		fy2 = parsedY2
	}
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

func pwDragBySelector(args map[string]string) []byte {
	fromSelector, ok := args["fromSelector"]
	if !ok || fromSelector == "" {
		return []byte(`{"error": "fromSelector not provided"}`)
	}
	toSelector, ok := args["toSelector"]
	if !ok || toSelector == "" {
		return []byte(`{"error": "toSelector not provided"}`)
	}
	if !browserStarted || page == nil {
		return []byte(`{"error": "Browser not started. Call pw_start first."}`)
	}
	fromJS := fmt.Sprintf(`
		function getCenter(selector) {
			const el = document.querySelector(selector);
			if (!el) return null;
			const rect = el.getBoundingClientRect();
			return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 };
		}
		getCenter(%q)
	`, fromSelector)
	toJS := fmt.Sprintf(`
		function getCenter(selector) {
			const el = document.querySelector(selector);
			if (!el) return null;
			const rect = el.getBoundingClientRect();
			return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 };
		}
		getCenter(%q)
	`, toSelector)
	fromResult, err := page.Evaluate(fromJS)
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to get from element: %s"}`, err.Error()))
	}
	fromMap, ok := fromResult.(map[string]interface{})
	if !ok || fromMap == nil {
		return []byte(fmt.Sprintf(`{"error": "from selector '%s' not found"}`, fromSelector))
	}
	fromX := fromMap["x"].(float64)
	fromY := fromMap["y"].(float64)
	toResult, err := page.Evaluate(toJS)
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to get to element: %s"}`, err.Error()))
	}
	toMap, ok := toResult.(map[string]interface{})
	if !ok || toMap == nil {
		return []byte(fmt.Sprintf(`{"error": "to selector '%s' not found"}`, toSelector))
	}
	toX := toMap["x"].(float64)
	toY := toMap["y"].(float64)
	mouse := page.Mouse()
	err = mouse.Move(fromX, fromY)
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to move mouse: %s"}`, err.Error()))
	}
	err = mouse.Down()
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to mouse down: %s"}`, err.Error()))
	}
	err = mouse.Move(toX, toY)
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to move mouse: %s"}`, err.Error()))
	}
	err = mouse.Up()
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to mouse up: %s"}`, err.Error()))
	}
	msg := fmt.Sprintf("Dragged from %s (%.0f,%.0f) to %s (%.0f,%.0f)", fromSelector, fromX, fromY, toSelector, toX, toY)
	return []byte(fmt.Sprintf(`{"success": true, "message": "%s"}`, msg))
}

// nolint:unused
func pwClickAt(args map[string]string) []byte {
	x, ok := args["x"]
	if !ok {
		return []byte(`{"error": "x not provided"}`)
	}
	y, ok := args["y"]
	if !ok {
		return []byte(`{"error": "y not provided"}`)
	}
	if !browserStarted || page == nil {
		return []byte(`{"error": "Browser not started. Call pw_start first."}`)
	}
	fx, err := strconv.ParseFloat(x, 64)
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to parse x: %s"}`, err.Error()))
	}
	fy, err := strconv.ParseFloat(y, 64)
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to parse y: %s"}`, err.Error()))
	}
	mouse := page.Mouse()
	err = mouse.Click(fx, fy)
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to click: %s"}`, err.Error()))
	}
	return []byte(fmt.Sprintf(`{"success": true, "message": "Clicked at (%s,%s)"}`, x, y))
}

func pwGetHTML(args map[string]string) []byte {
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
	html, err := locator.First().InnerHTML()
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to get HTML: %s"}`, err.Error()))
	}
	return []byte(fmt.Sprintf(`{"html": %s}`, jsonString(html)))
}

type DOMElement struct {
	Tag        string            `json:"tag,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Text       string            `json:"text,omitempty"`
	Children   []DOMElement      `json:"children,omitempty"`
	Selector   string            `json:"selector,omitempty"`
	InnerHTML  string            `json:"innerHTML,omitempty"`
}

func buildDOMTree(locator playwright.Locator) ([]DOMElement, error) {
	var results []DOMElement
	count, err := locator.Count()
	if err != nil {
		return nil, err
	}
	for i := 0; i < count; i++ {
		el := locator.Nth(i)
		dom, err := elementToDOM(el)
		if err != nil {
			continue
		}
		results = append(results, dom)
	}
	return results, nil
}

func elementToDOM(el playwright.Locator) (DOMElement, error) {
	dom := DOMElement{}
	tag, err := el.Evaluate(`el => el.nodeName`, nil)
	if err == nil {
		dom.Tag = strings.ToLower(fmt.Sprintf("%v", tag))
	}
	attributes := make(map[string]string)
	attrs, err := el.Evaluate(`el => {
		let attrs = {};
		for (let i = 0; i < el.attributes.length; i++) {
			let attr = el.attributes[i];
			attrs[attr.name] = attr.value;
		}
		return attrs;
	}`, nil)
	if err == nil {
		if amap, ok := attrs.(map[string]any); ok {
			for k, v := range amap {
				if vs, ok := v.(string); ok {
					attributes[k] = vs
				}
			}
		}
	}
	if len(attributes) > 0 {
		dom.Attributes = attributes
	}
	text, err := el.TextContent()
	if err == nil && text != "" {
		dom.Text = text
	}
	innerHTML, err := el.InnerHTML()
	if err == nil && innerHTML != "" {
		dom.InnerHTML = innerHTML
	}
	childCount, _ := el.Count()
	if childCount > 0 {
		childrenLocator := el.Locator("*")
		children, err := buildDOMTree(childrenLocator)
		if err == nil && len(children) > 0 {
			dom.Children = children
		}
	}
	return dom, nil
}

func pwGetDOM(args map[string]string) []byte {
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
	dom, err := elementToDOM(locator.First())
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to get DOM: %s"}`, err.Error()))
	}
	data, err := json.Marshal(dom)
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to marshal DOM: %s"}`, err.Error()))
	}
	return []byte(fmt.Sprintf(`{"dom": %s}`, string(data)))
}

// nolint:unused
func pwSearchElements(args map[string]string) []byte {
	text := args["text"]
	selector := args["selector"]
	if text == "" && selector == "" {
		return []byte(`{"error": "text or selector not provided"}`)
	}
	if !browserStarted || page == nil {
		return []byte(`{"error": "Browser not started. Call pw_start first."}`)
	}
	var locator playwright.Locator
	if text != "" {
		locator = page.GetByText(text)
	} else {
		locator = page.Locator(selector)
	}
	count, err := locator.Count()
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to search elements: %s"}`, err.Error()))
	}
	if count == 0 {
		return []byte(`{"elements": []}`)
	}
	var results []map[string]string
	for i := 0; i < count; i++ {
		el := locator.Nth(i)
		tag, _ := el.Evaluate(`el => el.nodeName`, nil)
		text, _ := el.TextContent()
		html, _ := el.InnerHTML()
		results = append(results, map[string]string{
			"index": strconv.Itoa(i),
			"tag":   strings.ToLower(fmt.Sprintf("%v", tag)),
			"text":  text,
			"html":  html,
		})
	}
	data, err := json.Marshal(results)
	if err != nil {
		return []byte(fmt.Sprintf(`{"error": "failed to marshal results: %s"}`, err.Error()))
	}
	return []byte(fmt.Sprintf(`{"elements": %s}`, string(data)))
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
