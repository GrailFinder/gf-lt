package main

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var (
	colorschemes = map[string]tview.Theme{
		"default": tview.Theme{
			PrimitiveBackgroundColor:    tcell.ColorDefault,
			ContrastBackgroundColor:     tcell.ColorGray,
			MoreContrastBackgroundColor: tcell.ColorSteelBlue,
			BorderColor:                 tcell.ColorGray,
			TitleColor:                  tcell.ColorRed,
			GraphicsColor:               tcell.ColorBlue,
			PrimaryTextColor:            tcell.ColorLightGray,
			SecondaryTextColor:          tcell.ColorYellow,
			TertiaryTextColor:           tcell.ColorOrange,
			InverseTextColor:            tcell.ColorPurple,
			ContrastSecondaryTextColor:  tcell.ColorLime,
		},
		"gruvbox": tview.Theme{
			PrimitiveBackgroundColor:    tcell.NewHexColor(0x282828), // Background: #282828 (dark gray)
			ContrastBackgroundColor:     tcell.ColorDarkGoldenrod,    // Selected option: warm yellow (#b57614)
			MoreContrastBackgroundColor: tcell.ColorDarkSlateGray,    // Non-selected options: dark grayish-blue (#32302f)
			BorderColor:                 tcell.ColorLightGray,        // Light gray (#a89984)
			TitleColor:                  tcell.ColorRed,              // Red (#fb4934)
			GraphicsColor:               tcell.ColorDarkCyan,         // Cyan (#689d6a)
			PrimaryTextColor:            tcell.ColorLightGray,        // Light gray (#d5c4a1)
			SecondaryTextColor:          tcell.ColorYellow,           // Yellow (#fabd2f)
			TertiaryTextColor:           tcell.ColorOrange,           // Orange (#fe8019)
			InverseTextColor:            tcell.ColorWhite,            // White (#f9f5d7) for selected text
			ContrastSecondaryTextColor:  tcell.ColorLightGreen,       // Light green (#b8bb26)
		},
		"solarized": tview.Theme{
			PrimitiveBackgroundColor:    tcell.NewHexColor(0x002b36), // Background: #002b36 (base03)
			ContrastBackgroundColor:     tcell.ColorDarkCyan,         // Selected option: cyan (#2aa198)
			MoreContrastBackgroundColor: tcell.ColorDarkSlateGray,    // Non-selected options: dark blue (#073642)
			BorderColor:                 tcell.ColorLightBlue,        // Light blue (#839496)
			TitleColor:                  tcell.ColorRed,              // Red (#dc322f)
			GraphicsColor:               tcell.ColorBlue,             // Blue (#268bd2)
			PrimaryTextColor:            tcell.ColorWhite,            // White (#fdf6e3)
			SecondaryTextColor:          tcell.ColorYellow,           // Yellow (#b58900)
			TertiaryTextColor:           tcell.ColorOrange,           // Orange (#cb4b16)
			InverseTextColor:            tcell.ColorWhite,            // White (#eee8d5) for selected text
			ContrastSecondaryTextColor:  tcell.ColorLightCyan,        // Light cyan (#93a1a1)
		},
		"dracula": tview.Theme{
			PrimitiveBackgroundColor:    tcell.NewHexColor(0x282a36), // Background: #282a36
			ContrastBackgroundColor:     tcell.ColorDarkMagenta,      // Selected option: magenta (#bd93f9)
			MoreContrastBackgroundColor: tcell.ColorDarkGray,         // Non-selected options: dark gray (#44475a)
			BorderColor:                 tcell.ColorLightGray,        // Light gray (#f8f8f2)
			TitleColor:                  tcell.ColorRed,              // Red (#ff5555)
			GraphicsColor:               tcell.ColorDarkCyan,         // Cyan (#8be9fd)
			PrimaryTextColor:            tcell.ColorWhite,            // White (#f8f8f2)
			SecondaryTextColor:          tcell.ColorYellow,           // Yellow (#f1fa8c)
			TertiaryTextColor:           tcell.ColorOrange,           // Orange (#ffb86c)
			InverseTextColor:            tcell.ColorWhite,            // White (#f8f8f2) for selected text
			ContrastSecondaryTextColor:  tcell.ColorLightGreen,       // Light green (#50fa7b)
		},
	}
)
