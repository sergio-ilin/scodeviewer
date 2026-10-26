package gui

import (
	"image/color"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"golang.org/x/exp/shiny/materialdesign/icons"
)

type Icons struct {
	Paperclip    *widget.Icon
	Copy         *widget.Icon
	Check        *widget.Icon
	ExpandMore   *widget.Icon
	ExpandLess   *widget.Icon
	ChevronRight *widget.Icon
	File         *widget.Icon
	Folder       *widget.Icon
	Edit         *widget.Icon
	Delete       *widget.Icon
	Merge        *widget.Icon
}

func NewIcons() *Icons {
	load := func(data []byte) *widget.Icon {
		ic, _ := widget.NewIcon(data)
		return ic
	}

	return &Icons{
		Paperclip:    load(icons.EditorAttachFile),
		Copy:         load(icons.ContentContentCopy),
		Check:        load(icons.NavigationCheck),
		ExpandMore:   load(icons.NavigationExpandMore),
		ExpandLess:   load(icons.NavigationExpandLess),
		ChevronRight: load(icons.NavigationChevronRight),
		File:         load(icons.EditorInsertDriveFile),
		Folder:       load(icons.FileFolder),
		Edit:         load(icons.EditorModeEdit),
		Delete:       load(icons.ActionDelete),
		Merge:        load(icons.EditorMergeType),
	}
}

func (ics *Icons) Layout(gtx layout.Context, icon *widget.Icon, col color.NRGBA, size unit.Dp) layout.Dimensions {
	if icon == nil {
		return layout.Dimensions{}
	}
	px := gtx.Dp(size)
	gtx.Constraints.Min.X = px
	gtx.Constraints.Min.Y = px
	gtx.Constraints.Max.X = px
	gtx.Constraints.Max.Y = px
	return icon.Layout(gtx, col)
}
