package main

var browserToolSysMsg = `
Additional browser automation tools (Playwright):
[
{
    "name": "pw_start",
    "args": [],
    "when_to_use": "start a browser instance before doing any browser automation. Must be called first."
},
{
    "name": "pw_stop",
    "args": [],
    "when_to_use": "stop the browser instance when done with automation."
},
{
    "name": "pw_is_running",
    "args": [],
    "when_to_use": "check if browser is currently running."
},
{
    "name": "pw_navigate",
    "args": ["url"],
    "when_to_use": "when asked to open a specific URL in a web browser."
},
{
    "name": "pw_click",
    "args": ["selector", "index"],
    "when_to_use": "when asked to click on an element on the current webpage. 'index' is optional (default 0) to handle multiple matches."
},
{
    "name": "pw_fill",
    "args": ["selector", "text", "index"],
    "when_to_use": "when asked to type text into an input field. 'index' is optional."
},
{
    "name": "pw_extract_text",
    "args": ["selector"],
    "when_to_use": "when asked to get text content from the page or specific elements. Use selector 'body' for all page text."
},
{
    "name": "pw_screenshot",
    "args": ["selector", "full_page"],
    "when_to_use": "when asked to take a screenshot of the page or a specific element. Returns a file path to the image."
},
{
    "name": "pw_screenshot_and_view",
    "args": ["selector", "full_page"],
    "when_to_use": "when asked to take a screenshot and show it to the model. Returns image for viewing."
},
{
    "name": "pw_wait_for_selector",
    "args": ["selector", "timeout"],
    "when_to_use": "when asked to wait for an element to appear on the page before proceeding."
},
{
    "name": "pw_drag",
    "args": ["x1", "y1", "x2", "y2"],
    "when_to_use": "drag the mouse from point (x1,y1) to (x2,y2)"
}
]
`
