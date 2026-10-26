package gui

import (
	"image/color"

	"golang.org/x/sys/windows/registry"
)

type ThemeMode int

const (
	ThemeAuto ThemeMode = iota
	ThemeDark
	ThemeLight
)

type Palette struct {
	IsDark    bool
	Bg        color.NRGBA
	Surface   color.NRGBA
	HeaderBg  color.NRGBA
	Border    color.NRGBA
	Text      color.NRGBA
	TextMuted color.NRGBA
	Accent    color.NRGBA
	BadgeBg   color.NRGBA
	BadgeText color.NRGBA
	AddBg     color.NRGBA
	AddText   color.NRGBA
	DelBg     color.NRGBA
	DelText   color.NRGBA
	Selected  color.NRGBA
	Hover     color.NRGBA
}

func IsSystemDark() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return true
	}
	defer k.Close()
	val, _, err := k.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		return true
	}
	return val == 0
}

func GetPalette(mode ThemeMode) Palette {
	isDark := true
	switch mode {
	case ThemeAuto:
		isDark = IsSystemDark()
	case ThemeDark:
		isDark = true
	case ThemeLight:
		isDark = false
	}

	if isDark {
		return Palette{
			IsDark:    true,
			Bg:        color.NRGBA{R: 13, G: 17, B: 23, A: 255},
			Surface:   color.NRGBA{R: 22, G: 27, B: 34, A: 255},
			HeaderBg:  color.NRGBA{R: 18, G: 22, B: 28, A: 255},
			Border:    color.NRGBA{R: 48, G: 54, B: 61, A: 255},
			Text:      color.NRGBA{R: 230, G: 237, B: 243, A: 255},
			TextMuted: color.NRGBA{R: 139, G: 148, B: 158, A: 255},
			Accent:    color.NRGBA{R: 47, G: 129, B: 247, A: 255},
			BadgeBg:   color.NRGBA{R: 33, G: 38, B: 45, A: 255},
			BadgeText: color.NRGBA{R: 121, G: 192, B: 255, A: 255},
			AddBg:     color.NRGBA{R: 3, G: 58, B: 22, A: 120},
			AddText:   color.NRGBA{R: 63, G: 185, B: 80, A: 255},
			DelBg:     color.NRGBA{R: 103, G: 6, B: 12, A: 120},
			DelText:   color.NRGBA{R: 248, G: 81, B: 73, A: 255},
			Selected:  color.NRGBA{R: 56, G: 139, B: 253, A: 38},
			Hover:     color.NRGBA{R: 48, G: 54, B: 61, A: 100},
		}
	}

	return Palette{
		IsDark:    false,
		Bg:        color.NRGBA{R: 255, G: 255, B: 255, A: 255},
		Surface:   color.NRGBA{R: 246, G: 248, B: 250, A: 255},
		HeaderBg:  color.NRGBA{R: 240, G: 243, B: 246, A: 255},
		Border:    color.NRGBA{R: 208, G: 215, B: 222, A: 255},
		Text:      color.NRGBA{R: 31, G: 35, B: 40, A: 255},
		TextMuted: color.NRGBA{R: 101, G: 109, B: 118, A: 255},
		Accent:    color.NRGBA{R: 9, G: 105, B: 218, A: 255},
		BadgeBg:   color.NRGBA{R: 221, G: 244, B: 255, A: 255},
		BadgeText: color.NRGBA{R: 9, G: 105, B: 218, A: 255},
		AddBg:     color.NRGBA{R: 230, G: 255, B: 236, A: 255},
		AddText:   color.NRGBA{R: 26, G: 127, B: 55, A: 255},
		DelBg:     color.NRGBA{R: 255, G: 235, B: 233, A: 255},
		DelText:   color.NRGBA{R: 207, G: 34, B: 46, A: 255},
		Selected:  color.NRGBA{R: 221, G: 244, B: 255, A: 180},
		Hover:     color.NRGBA{R: 240, G: 243, B: 246, A: 255},
	}
}
