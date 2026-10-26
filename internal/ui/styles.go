package ui

import "github.com/gdamore/tcell/v2"

type styles struct {
	bg          tcell.Style
	status      tcell.Style
	header      tcell.Style
	text        tcell.Style
	selected    tcell.Style
	dim         tcell.Style
	hint        tcell.Style
	lockBg      tcell.Style
	lockTitle   tcell.Style
	playerIcon  tcell.Style
	playerTitle tcell.Style
	progress    tcell.Style
}

func newStyles() styles {
	bg := tcell.NewRGBColor(0x0b, 0x0e, 0x14)
	fg := tcell.NewRGBColor(0xf2, 0xf4, 0xf8)
	dim := tcell.NewRGBColor(0x8b, 0x94, 0xa7)
	accent := tcell.NewRGBColor(0xff, 0xc4, 0x00)
	selection := tcell.NewRGBColor(0x0f, 0x78, 0xd7)
	return styles{
		bg:          tcell.StyleDefault.Background(bg).Foreground(fg),
		status:      tcell.StyleDefault.Background(bg).Foreground(fg).Bold(true),
		header:      tcell.StyleDefault.Background(bg).Foreground(accent).Bold(true),
		text:        tcell.StyleDefault.Background(bg).Foreground(fg),
		selected:    tcell.StyleDefault.Background(selection).Foreground(fg).Bold(true),
		dim:         tcell.StyleDefault.Background(bg).Foreground(dim),
		hint:        tcell.StyleDefault.Background(bg).Foreground(dim),
		lockBg:      tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(fg),
		lockTitle:   tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(accent).Bold(true),
		playerIcon:  tcell.StyleDefault.Background(bg).Foreground(accent).Bold(true),
		playerTitle: tcell.StyleDefault.Background(bg).Foreground(fg).Bold(true),
		progress:    tcell.StyleDefault.Background(bg).Foreground(accent),
	}
}
