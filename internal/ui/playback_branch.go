package tvui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
)

const (
	playerBranchControl app.FocusID = "player/branch"
	playerBranchGroup               = "player-branch"
)

func playerBranchOptionID(index int) app.FocusID {
	return app.FocusID(fmt.Sprintf("player/branch/%d", index))
}

func playbackHasBranchOverlay(state app.PlaybackState) bool {
	branch := state.Resolution.Plan.Branch
	return state.BranchPending && branch != nil && len(branch.Options) > 0 && !branch.AutoAdvance
}

func playbackHasBranchChoices(state app.PlaybackState) bool {
	branch := state.Resolution.Plan.Branch
	return branch != nil && len(branch.Options) > 0 && !branch.AutoAdvance
}

func playbackAutoAdvancingBranch(state app.PlaybackState) bool {
	branch := state.Resolution.Plan.Branch
	return branch != nil && branch.AutoAdvance && len(branch.Options) == 1
}

func defaultBranchOptionIndex(state app.PlaybackState) int {
	branch := state.Resolution.Plan.Branch
	if branch == nil {
		return 0
	}
	for i, opt := range branch.Options {
		if opt.Default {
			return i
		}
	}
	return 0
}

func isValidBranchFocus(focus app.FocusID, count int) bool {
	raw, ok := strings.CutPrefix(string(focus), "player/branch/")
	if !ok {
		return false
	}
	idx, err := strconv.Atoi(raw)
	return err == nil && idx >= 0 && idx < count
}

func (p *PageContext) drawPlaybackBranch(c *ui.Context, state app.PlaybackState) {
	v, s := p.host, &p.host.player
	branch := state.Resolution.Plan.Branch
	if branch == nil || len(branch.Options) == 0 {
		return
	}
	options := branch.Options
	if p.active && !isValidBranchFocus(v.State.Focus, len(options)) {
		v.State.Focus = playerBranchOptionID(defaultBranchOptionIndex(state))
		v.reveal = true
	}
	colors, d := p.Theme.Colors, p.uiColors()
	text := p.Assets.Text.Playback
	question := branch.Question
	if question == "" {
		question = text.BranchQuestion
	}
	p.region(playerBranchGroup, focusEdges{})

	const (
		padX     = float32(150)
		padTop   = float32(90)
		padBot   = float32(110)
		colGap   = float32(30)
		rowGap   = float32(24)
		cardMinH = float32(136)
	)
	contentWidth := max(400, p.Width-2*padX)
	cols := 2
	if len(options) == 1 {
		cols = 1
	}
	cardWidth := (contentWidth - colGap) / 2
	if cols == 1 {
		cardWidth = min(cardWidth, contentWidth)
	}

	ui.Box(c.Key("playback-branch")).Absolute().Top(0).Left(0).Fill().ZIndex(15).
		LinearGradient(ui.LinearGradient{
			Angle: 0,
			Stops: []ui.GradientStop{
				{Color: withAlpha(colors.Surface.Canvas, 0xfc)},
				{Color: withAlpha(colors.Surface.Canvas, 0xd4), At: 0.46},
				{Color: withAlpha(colors.Surface.Canvas, 0x54), At: 0.74},
				{Color: ui.Transparent, At: 0.94},
			},
		}).Children(func() {
		ui.Column(c).Fill().Padding(padTop, padX, padBot, padX).Justify(ui.End).AlignItems(ui.Center).Children(func() {
			ui.Text(c, text.Interactive).FontSize(18).LetterSpacing(4).TextAlign(ui.Center).
				TextColor(paint(colors.Accent.Soft))
			ui.Text(c, question).FillWidth().Margin(23, 0, 42, 0).FontSize(46).FontWeight(450).
				TextAlign(ui.Center).MaxLines(2).TextColor(paint(colors.Playback.Foreground))
			rows := (len(options) + cols - 1) / cols
			ui.Column(c.Key("playback-branch-options")).FillWidth().Gap(rowGap).AlignItems(ui.Center).Children(func() {
				for row := 0; row < rows; row++ {
					start := row * cols
					end := min(start+cols, len(options))
					ui.Row(c.Key(fmt.Sprintf("playback-branch-row-%d", row))).FillWidth().Gap(colGap).
						Justify(ui.Center).AlignItems(ui.Stretch).Children(func() {
						for idx := start; idx < end; idx++ {
							opt := options[idx]
							id := playerBranchOptionID(idx)
							focused := p.paintFocus() == id
							ordinal := fmt.Sprintf("%02d", idx+1)
							fill := withAlpha(colors.Surface.Raised, 0xc9)
							border := withAlpha(colors.Playback.Foreground, 0x28)
							borderWidth := float32(1)
							numInk := paint(colors.Accent.Soft)
							titleInk := paint(colors.Playback.Foreground)
							subInk := paint(colors.Playback.Secondary)
							if focused {
								fill = paint(colors.Focus.Background)
								border = paint(colors.Focus.Ring)
								borderWidth = 3
								numInk = d.focusSubtext
								titleInk = paint(colors.Focus.Foreground)
								subInk = d.focusSubtext
							}
							optionID := opt.ID
							btn := ui.Button(c.Key(string(id)), "").Label(ordinal+" "+opt.Title).
								Width(cardWidth).MinHeight(cardMinH).Padding(28, 30).Radius(19).
								Background(fill).Border(borderWidth, border).
								OnClick(func() {
									p.dispatch(func() {
										if v.Playback.SelectBranch(optionID) {
											s.panel = ""
											s.visible = false
											v.State.Focus, v.reveal = playerWake, true
										}
									})
								})
							if focused {
								btn.Shadow(0, 8, 36, 0, withAlpha(colors.Overlay.Shadow, 0x66))
							}
							p.bind(btn, id, playerBranchGroup)
							btn.Children(func() {
								ui.Row(c).FillWidth().Gap(26).AlignItems(ui.Center).Children(func() {
									ui.Text(c, ordinal).FontSize(39).FontWeight(300).Shrink(0).TextColor(numInk)
									ui.Column(c).Grow(1).Shrink(1).Gap(10).Children(func() {
										ui.Text(c, opt.Title).FillWidth().FontSize(29).FontWeight(450).MaxLines(2).TextColor(titleInk)
										if opt.Subtitle != "" {
											ui.Text(c, opt.Subtitle).FillWidth().FontSize(20).MaxLines(2).TextColor(subInk)
										}
									})
								})
							})
						}
					})
				}
			})
		})
	})
}
