package tools

import "gf-lt/models"

// Window tools (xdotool + maim). They are only registered when both binaries are
// on PATH - see registerWindowTools().
//
// capture_window_and_view additionally requires a vision-capable model, since it
// returns the image itself rather than a path.

var windowArgProps = map[string]models.ToolArgProps{
	"window": models.ToolArgProps{
		Type:        "string",
		Description: "window id (decimal or hex) or a substring of the window name",
	},
}

var windowListToolDef = models.Tool{
	Type: "function",
	Function: models.ToolFunc{
		Name:        "list_windows",
		Description: "List available X11 windows as a JSON object of {id: name}. Requires xdotool.",
		Parameters: models.ToolFuncParams{
			Type:       "object",
			Required:   []string{},
			Properties: map[string]models.ToolArgProps{},
		},
	},
}

var captureWindowToolDef = models.Tool{
	Type: "function",
	Function: models.ToolFunc{
		Name:        "capture_window",
		Description: "Capture a screenshot of a window to /tmp and return the path. Requires xdotool and maim.",
		Parameters: models.ToolFuncParams{
			Type:     "object",
			Required: []string{"window"},
			Properties: map[string]models.ToolArgProps{
				"window": windowArgProps["window"],
			},
		},
	},
}

var captureWindowAndViewToolDef = models.Tool{
	Type: "function",
	Function: models.ToolFunc{
		Name:        "capture_window_and_view",
		Description: "Capture a window screenshot and return it as an image for direct inspection. Requires xdotool, maim, and a vision-capable model.",
		Parameters: models.ToolFuncParams{
			Type:     "object",
			Required: []string{"window"},
			Properties: map[string]models.ToolArgProps{
				"window": windowArgProps["window"],
			},
		},
	},
}
