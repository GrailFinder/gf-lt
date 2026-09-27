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

func pwStart(args map[string]string) ([]byte, error) {
	browserStartMu.Lock()
	defer browserStartMu.Unlock()
	if browserStarted {
		// Not a failure: start is idempotent, and reporting an already-running
		// browser as an error used to spend a mission failure for doing the right
		// thing. (Its mirror, stop-when-stopped, was already a success.)
		return []byte(`{"success": true, "message": "Browser was already started"}`), nil
	}
	if pw == nil {
		if err := CheckPlaywright(); err != nil {
			return nil, models.Unavailable("playwright is not available", "install the playwright browsers, or enable Playwright in config")
		}
	}
	var err error
	browser, err = pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(!cfg.PlaywrightDebug),
	})
	if err != nil {
		return nil, models.Internal("failed to launch the browser", err)
	}
	page, err = browser.NewPage()
	if err != nil {
		browser.Close()
		return nil, models.Internal("failed to open a page", err)
	}
	browserStarted = true
	return []byte(`{"success": true, "message": "Browser started"}`), nil
}

func pwStop(args map[string]string) ([]byte, error) {
	browserStartMu.Lock()
	defer browserStartMu.Unlock()
	if !browserStarted {
		return []byte(`{"success": true, "message": "Browser was not running"}`), nil
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
	return []byte(`{"success": true, "message": "Browser stopped"}`), nil
}

func pwIsRunning(args map[string]string) ([]byte, error) {
	if browserStarted {
		return []byte(`{"running": true, "message": "Browser is running"}`), nil
	}
	return []byte(`{"running": false, "message": "Browser is not running"}`), nil
}

func pwNavigate(args map[string]string) ([]byte, error) {
	url, ok := args["url"]
	if !ok || url == "" {
		return nil, models.InvalidArgs("url is required", "browser go <url>")
	}
	if !browserStarted || page == nil {
		return nil, browserNotStarted()
	}
	_, err := page.Goto(url)
	if err != nil {
		return nil, pwCallErr("navigate", err)
	}
	title, _ := page.Title()
	pageURL := page.URL()
	return []byte(fmt.Sprintf(`{"success": true, "title": "%s", "url": "%s"}`, title, pageURL)), nil
}

func pwClick(args map[string]string) ([]byte, error) {
	selector, ok := args["selector"]
	if !ok || selector == "" {
		return nil, models.InvalidArgs("selector is required", "pass a CSS selector, e.g. #submit or .result-row")
	}
	if !browserStarted || page == nil {
		return nil, browserNotStarted()
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
		return nil, models.Internal("could not query the page for elements", err)
	}
	if index >= count {
		return nil, models.NotFound(fmt.Sprintf("no element at index %d (found %d)", index, count), "use an index within range, or omit it to click the first match")
	}
	err = locator.Nth(index).Click()
	if err != nil {
		return nil, pwCallErr("click", err)
	}
	return []byte(`{"success": true, "message": "Clicked element"}`), nil
}

func pwFill(args map[string]string) ([]byte, error) {
	selector, ok := args["selector"]
	if !ok || selector == "" {
		return nil, models.InvalidArgs("selector is required", "pass a CSS selector, e.g. #submit or .result-row")
	}
	text := args["text"]
	if text == "" {
		text = ""
	}
	if !browserStarted || page == nil {
		return nil, browserNotStarted()
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
		return nil, models.Internal("could not query the page for elements", err)
	}
	if index >= count {
		return nil, models.NotFound(fmt.Sprintf("no element at index %d", index), "omit the index to click the first match")
	}
	err = locator.Nth(index).Fill(text)
	if err != nil {
		return nil, pwCallErr("fill", err)
	}
	return []byte(`{"success": true, "message": "Filled input"}`), nil
}

func pwExtractText(args map[string]string) ([]byte, error) {
	selector := args["selector"]
	if selector == "" {
		selector = "body"
	}
	if !browserStarted || page == nil {
		return nil, browserNotStarted()
	}
	locator := page.Locator(selector)
	count, err := locator.Count()
	if err != nil {
		return nil, models.Internal("could not query the page for elements", err)
	}
	if count == 0 {
		return nil, models.NotFound("no elements matched", "check the selector, or take a screenshot to see the current page")
	}
	if selector == "body" {
		text, err := page.Locator("body").TextContent()
		if err != nil {
			return nil, pwCallErr("read text", err)
		}
		return []byte(fmt.Sprintf(`{"text": "%s"}`, text)), nil
	}
	// A node can vanish between Count() and TextContent() on a live page, so
	// partial extraction is the normal case, not an edge case. The extracted
	// text is returned alongside the error rather than being discarded - which
	// is the whole reason the handler signature carries both.
	var texts []string
	var failures int
	for i := 0; i < count; i++ {
		text, err := locator.Nth(i).TextContent()
		if err != nil {
			failures++
			if logger != nil {
				logger.Debug("pwExtractText: element unreadable", "index", i, "error", err)
			}
			continue
		}
		texts = append(texts, text)
	}
	out := []byte(fmt.Sprintf(`{"text": "%s"}`, joinLines(texts)))
	if failures > 0 {
		return out, &models.ToolError{
			Code: models.CodeConflict,
			Msg:  fmt.Sprintf("extracted %d of %d elements; %d could not be read", len(texts), count, failures),
			Hint: "the text above is what was readable; the page may have changed, so re-read before relying on it",
		}
	}
	return out, nil
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

func pwScreenshot(args map[string]string) ([]byte, error) {
	selector := args["selector"]
	fullPage := args["full_page"] == "true"
	if !browserStarted || page == nil {
		return nil, browserNotStarted()
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
		return nil, pwCallErr("take a screenshot", err)
	}
	return []byte(fmt.Sprintf(`{"path": "%s"}`, path)), nil
}

func pwScreenshotAndView(args map[string]string) ([]byte, error) {
	selector := args["selector"]
	fullPage := args["full_page"] == "true"
	if !browserStarted || page == nil {
		return nil, browserNotStarted()
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
		return nil, pwCallErr("take a screenshot", err)
	}
	dataURL, err := models.CreateImageURLFromPath(path)
	if err != nil {
		return nil, models.Internal("failed to encode the screenshot", err)
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
		return nil, models.Internal("failed to encode the screenshot result", err)
	}
	return jsonResult, nil
}

func pwWaitForSelector(args map[string]string) ([]byte, error) {
	selector, ok := args["selector"]
	if !ok || selector == "" {
		return nil, models.InvalidArgs("selector is required", "pass a CSS selector, e.g. #submit or .result-row")
	}
	if !browserStarted || page == nil {
		return nil, browserNotStarted()
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
		return nil, models.NotFound("the element was not found on the page", "re-read the page with `browser text` and retry")
	}
	return []byte(`{"success": true, "message": "Element found"}`), nil
}

func pwDrag(args map[string]string) ([]byte, error) {
	x1, ok := args["x1"]
	if !ok {
		return nil, models.InvalidArgs("x1 is required", "")
	}
	y1, ok := args["y1"]
	if !ok {
		return nil, models.InvalidArgs("y1 is required", "")
	}
	x2, ok := args["x2"]
	if !ok {
		return nil, models.InvalidArgs("x2 is required", "")
	}
	y2, ok := args["y2"]
	if !ok {
		return nil, models.InvalidArgs("y2 is required", "")
	}
	if !browserStarted || page == nil {
		return nil, browserNotStarted()
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
		return nil, pwCallErr("move the mouse", err)
	}
	err = mouse.Down()
	if err != nil {
		return nil, pwCallErr("press the mouse button", err)
	}
	err = mouse.Move(fx2, fy2)
	if err != nil {
		return nil, pwCallErr("move the mouse", err)
	}
	err = mouse.Up()
	if err != nil {
		return nil, pwCallErr("release the mouse button", err)
	}
	return []byte(fmt.Sprintf(`{"success": true, "message": "Dragged from (%s,%s) to (%s,%s)"}`, x1, y1, x2, y2)), nil
}

func pwDragBySelector(args map[string]string) ([]byte, error) {
	fromSelector, ok := args["fromSelector"]
	if !ok || fromSelector == "" {
		return nil, models.InvalidArgs("fromSelector is required", "")
	}
	toSelector, ok := args["toSelector"]
	if !ok || toSelector == "" {
		return nil, models.InvalidArgs("toSelector is required", "")
	}
	if !browserStarted || page == nil {
		return nil, browserNotStarted()
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
		return nil, pwCallErr("resolve the drag source", err)
	}
	fromMap, ok := fromResult.(map[string]interface{})
	if !ok || fromMap == nil {
		return nil, models.NotFound(fmt.Sprintf("drag source %q matched nothing", fromSelector), "check the selector with `browser html`")
	}
	fromX := fromMap["x"].(float64)
	fromY := fromMap["y"].(float64)
	toResult, err := page.Evaluate(toJS)
	if err != nil {
		return nil, pwCallErr("resolve the drag target", err)
	}
	toMap, ok := toResult.(map[string]interface{})
	if !ok || toMap == nil {
		return nil, models.NotFound(fmt.Sprintf("drag target %q matched nothing", toSelector), "check the selector with `browser html`")
	}
	toX := toMap["x"].(float64)
	toY := toMap["y"].(float64)
	mouse := page.Mouse()
	err = mouse.Move(fromX, fromY)
	if err != nil {
		return nil, pwCallErr("move the mouse", err)
	}
	err = mouse.Down()
	if err != nil {
		return nil, pwCallErr("press the mouse button", err)
	}
	err = mouse.Move(toX, toY)
	if err != nil {
		return nil, pwCallErr("move the mouse", err)
	}
	err = mouse.Up()
	if err != nil {
		return nil, pwCallErr("release the mouse button", err)
	}
	msg := fmt.Sprintf("Dragged from %s (%.0f,%.0f) to %s (%.0f,%.0f)", fromSelector, fromX, fromY, toSelector, toX, toY)
	return []byte(fmt.Sprintf(`{"success": true, "message": "%s"}`, msg)), nil
}

// nolint:unused
func pwClickAt(args map[string]string) ([]byte, error) {
	x, ok := args["x"]
	if !ok {
		return nil, models.InvalidArgs("x is required", "")
	}
	y, ok := args["y"]
	if !ok {
		return nil, models.InvalidArgs("y is required", "")
	}
	if !browserStarted || page == nil {
		return nil, browserNotStarted()
	}
	fx, err := strconv.ParseFloat(x, 64)
	if err != nil {
		return nil, models.InvalidArgs(fmt.Sprintf("x is not a number: %s", err), "pass a numeric coordinate")
	}
	fy, err := strconv.ParseFloat(y, 64)
	if err != nil {
		return nil, models.InvalidArgs(fmt.Sprintf("y is not a number: %s", err), "pass a numeric coordinate")
	}
	mouse := page.Mouse()
	err = mouse.Click(fx, fy)
	if err != nil {
		return nil, pwCallErr("click", err)
	}
	return []byte(fmt.Sprintf(`{"success": true, "message": "Clicked at (%s,%s)"}`, x, y)), nil
}

func pwGetHTML(args map[string]string) ([]byte, error) {
	selector := args["selector"]
	if selector == "" {
		selector = "body"
	}
	if !browserStarted || page == nil {
		return nil, browserNotStarted()
	}
	locator := page.Locator(selector)
	count, err := locator.Count()
	if err != nil {
		return nil, models.Internal("could not query the page for elements", err)
	}
	if count == 0 {
		return nil, models.NotFound("no elements matched", "check the selector, or take a screenshot to see the current page")
	}
	html, err := locator.First().InnerHTML()
	if err != nil {
		return nil, pwCallErr("read HTML", err)
	}
	return []byte(fmt.Sprintf(`{"html": %s}`, jsonString(html))), nil
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

func pwGetDOM(args map[string]string) ([]byte, error) {
	selector := args["selector"]
	if selector == "" {
		selector = "body"
	}
	if !browserStarted || page == nil {
		return nil, browserNotStarted()
	}
	locator := page.Locator(selector)
	count, err := locator.Count()
	if err != nil {
		return nil, models.Internal("could not query the page for elements", err)
	}
	if count == 0 {
		return nil, models.NotFound("no elements matched", "check the selector, or take a screenshot to see the current page")
	}
	dom, err := elementToDOM(locator.First())
	if err != nil {
		return nil, pwCallErr("read the DOM", err)
	}
	data, err := json.Marshal(dom)
	if err != nil {
		return nil, models.Internal("failed to encode the DOM", err)
	}
	return []byte(fmt.Sprintf(`{"dom": %s}`, string(data))), nil
}

// nolint:unused
func pwSearchElements(args map[string]string) ([]byte, error) {
	text := args["text"]
	selector := args["selector"]
	if text == "" && selector == "" {
		return nil, models.InvalidArgs("text or selector is required", "")
	}
	if !browserStarted || page == nil {
		return nil, browserNotStarted()
	}
	var locator playwright.Locator
	if text != "" {
		locator = page.GetByText(text)
	} else {
		locator = page.Locator(selector)
	}
	count, err := locator.Count()
	if err != nil {
		return nil, models.Internal("could not search the page", err)
	}
	if count == 0 {
		return []byte(`{"elements": []}`), nil
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
		return nil, models.Internal("failed to encode the search results", err)
	}
	return []byte(fmt.Sprintf(`{"elements": %s}`, string(data))), nil
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// browserNotStarted is the "the call was well-formed but the browser is not
// running" error. It used to be the same hand-written string in thirteen places,
// telling the model to run `pw_start` - a command that does not exist, not as a
// tool, not as a bash verb, not as a browser action. The verb is `start`.
//
// It is a models.Conflict rather than invalid_args: nothing about the call was
// malformed, the world was simply not in the state it requires, and the fix is
// one tool call away.
func browserNotStarted() *models.ToolError {
	return &models.ToolError{
		Code: models.CodeConflict,
		Msg:  "the browser is not running",
		Hint: "call `browser start` first",
	}
}

// pwCallErr classifies a failed Playwright interaction. The arguments were fine
// and the browser was up, so this is a models.Conflict: the page was not in the state
// the call needed. Playwright's own message is kept as the cause for the log and
// is deliberately not shown to the model, which gets the recovery step instead.
func pwCallErr(what string, err error) *models.ToolError {
	return &models.ToolError{
		Code: models.CodeConflict,
		Msg:  "failed to " + what,
		Hint: "re-read the page with `browser text` or `browser html` to see its current state, then retry",
		Err:  err,
	}
}

// appendMultimodalErrorPart embeds a models.ToolError as a leading text part of a
// multimodal payload, keeping the existing parts (notably the image) intact.
//
// This is the option-(b) arrangement: a multimodal tool result is recognised by
// its prefix downstream, so a prepended text header would make the image
// unreadable. The error therefore travels inside the payload where the model
// reads it, while the handler still returns a real error to the host.
func appendMultimodalErrorPart(payload []byte, err error) ([]byte, error) {
	var resp models.MultimodalToolResp
	if uerr := json.Unmarshal(payload, &resp); uerr != nil || resp.Type != "multimodal_content" {
		return payload, err
	}
	parts := append([]map[string]string{models.MultimodalErrorPart(err)}, resp.Parts...)
	return json.Marshal(models.MultimodalToolResp{Type: resp.Type, Parts: parts})
}
