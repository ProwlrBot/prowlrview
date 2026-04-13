// Package theme loads TOML themes and maps severities + UI regions to colors.
package theme

import (
	"github.com/gdamore/tcell/v2"
)

type Theme struct {
	Name        string
	Background  tcell.Color
	Foreground  tcell.Color
	Border      tcell.Color
	Accent      tcell.Color
	Title       tcell.Color
	SevCritical tcell.Color
	SevHigh     tcell.Color
	SevMedium   tcell.Color
	SevLow      tcell.Color
	SevInfo     tcell.Color
}

// Builtin returns shipped themes.
func Builtin() map[string]*Theme {
	return map[string]*Theme{
		"prowlr": {
			Name: "prowlr", Background: tcell.ColorBlack, Foreground: tcell.ColorWhite,
			Border: hex(0xff6ac1), Accent: hex(0x00eaff), Title: hex(0xff6ac1),
			SevCritical: hex(0xff2d55), SevHigh: hex(0xff9500),
			SevMedium: hex(0xffd60a), SevLow: hex(0x30d158), SevInfo: hex(0x8e8e93),
		},
		"cyberpunk": {
			Name: "cyberpunk", Background: tcell.ColorBlack, Foreground: hex(0xf6f8fa),
			Border: hex(0xff00a0), Accent: hex(0x00ffff), Title: hex(0xfcee0c),
			SevCritical: hex(0xff003c), SevHigh: hex(0xff6c11),
			SevMedium: hex(0xfcee0c), SevLow: hex(0x00ff9f), SevInfo: hex(0x7a7a7a),
		},
		"dracula": {
			Name: "dracula", Background: hex(0x282a36), Foreground: hex(0xf8f8f2),
			Border: hex(0xbd93f9), Accent: hex(0x8be9fd), Title: hex(0xff79c6),
			SevCritical: hex(0xff5555), SevHigh: hex(0xffb86c),
			SevMedium: hex(0xf1fa8c), SevLow: hex(0x50fa7b), SevInfo: hex(0x6272a4),
		},
		"nightshade": {
			Name: "nightshade", Background: hex(0x0a0118), Foreground: hex(0xe6e6e6),
			Border: hex(0x8a2be2), Accent: hex(0x00ced1), Title: hex(0xff1493),
			SevCritical: hex(0xff0066), SevHigh: hex(0xff8c00),
			SevMedium: hex(0xffd700), SevLow: hex(0x32cd32), SevInfo: hex(0x708090),
		},
		"solarized": {
			Name: "solarized", Background: hex(0x002b36), Foreground: hex(0x93a1a1),
			Border: hex(0x268bd2), Accent: hex(0x2aa198), Title: hex(0xd33682),
			SevCritical: hex(0xdc322f), SevHigh: hex(0xcb4b16),
			SevMedium: hex(0xb58900), SevLow: hex(0x859900), SevInfo: hex(0x586e75),
		},
	}
}

func hex(n int32) tcell.Color {
	return tcell.NewRGBColor((n>>16)&0xff, (n>>8)&0xff, n&0xff)
}
