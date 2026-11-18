package tvui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

func (p *PageContext) catalogPodium(c *ui.Context, section catalogSection) {
	const gap, numberWidth, numberGap = 36, 60, 14
	const widthCount = 3
	// Balance the different side bearings of the three visible labels.
	numberShifts := [...]float32{4, -1, 0}
	colors, labels := p.Theme.Colors, p.Assets.Text.Home
	width := (p.Width - section.grid.left - homeGridRight - (widthCount-1)*gap) / widthCount
	size := catalogCardSize{width: max(1, width-numberWidth), height: 287, cover: 205, title: 23}
	ui.Row(c.Key("ranking-podium")).Absolute().Left(section.grid.left).Right(homeGridRight).Top(5).
		Gap(gap).Children(func() {
		for index, item := range section.group.Items[:min(len(section.cards), section.grid.podium)] {
			ui.Box(c.Key("podium/" + string(section.cards[index]))).Width(width).Height(335).Shrink(0).Children(func() {
				ui.Text(c, fmt.Sprintf("%02d", index+1)).Absolute().Left(-numberGap + numberShifts[index]).Top(0).Width(numberWidth).
					SingleLine().TextAlign(ui.End).FontFeatures("pnum").FontSize(56).FontWeight(350).
					LetterSpacing(-4).TextColor(paint(colors.Accent.Soft))
				ui.Box(c).Absolute().Top(0).Left(numberWidth).Children(func() {
					p.catalogCard(c, section.cards[index], homeCards, item, size, nil)
				})
				if item.RankScore != nil {
					ui.Text(c, fmt.Sprintf("%s %s", labels.RankingHeat, catalogViews(*item.RankScore, labels))).
						Absolute().Left(numberWidth).Top(300).FontSize(16).TextColor(paint(colors.Surface.Raised))
				}
			})
		}
	})
}

func (p *PageContext) catalogRankingRow(c *ui.Context, id app.FocusID, item provider.MediaItem, rank int, width float32, direction func(app.Direction) bool) {
	colors, labels := p.Theme.Colors, p.Assets.Text.Home
	background, foreground, secondary := ui.Transparent, paint(colors.Text.Primary), paint(colors.Surface.Raised)
	if p.State.Focus == id {
		background, foreground, secondary = paint(colors.Focus.Background), paint(colors.Focus.Foreground), paint(colors.Accent.Foreground)
	}
	row := ui.Button(c.Key(string(id)), "").Label(item.Title).Size(width, rankingGridMetrics.height).
		Radius(8).Border(0, ui.Transparent).Padding(0, 0).Shrink(0).Background(background).TextColor(foreground).
		OnClick(func() { p.dispatch(func() { p.openCatalogAction(labels.Details, &item, true) }) })
	p.animateFocus(row, background, foreground)
	p.bindDirection(row, id, homeCards, direction)
	row.Children(func() {
		ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(1).Background(paint(colors.Border.Subtle))
		ui.Row(c).Fill().Padding(0, 12).Gap(18).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("%02d", rank)).Width(45).Shrink(0).FontSize(28).TextColor(secondary)
			ui.Box(c).Size(120, rankingGridMetrics.cover).Shrink(0).Clip().Radius(7).
				Background(paint(colors.Surface.Raised)).Children(func() {
				if image := p.catalogCover(item.CoverURL); image != nil {
					ui.Image(c, image).Fill().Fit(ui.Cover)
				}
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(7).Children(func() {
				ui.Text(c, item.Title).FillWidth().SingleLine().FontSize(21).FontWeight(450)
				metadata := item.Category
				if item.Creator != nil {
					metadata = strings.TrimSuffix(item.Creator.Name+" \u00b7 "+metadata, " \u00b7 ")
				}
				if metadata != "" {
					ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Center).Children(func() {
						if item.Creator != nil {
							p.catalogCreatorPortrait(c, item.Creator.AvatarURL)
						}
						ui.Text(c, metadata).Shrink(1).SingleLine().FontSize(15).TextColor(secondary)
					})
				}
			})
			if item.Views != nil {
				ui.Text(c, catalogViews(*item.Views, labels)+labels.Plays).SingleLine().Shrink(0).
					FontSize(16).TextColor(secondary)
			}
		})
	})
}
