package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// 动画都很短（120–250ms），只用位移、透明度、缩放和颜色，不会有空闲时一直运行的循环。
// 列表滚动时不给单行加动画（虚拟列表的行没有 Enter/Position 过渡），新邮件高亮只改背景色，
// 并在结束时用 c.After 只安排一帧。系统开启“减少动态效果”时全部关闭。

const (
	pressDur  = 120 * time.Millisecond
	colorDur  = 120 * time.Millisecond
	detailDur = 200 * time.Millisecond
	dialogDur = 180 * time.Millisecond
	switchDur = 160 * time.Millisecond
	freshDur  = 2400 * time.Millisecond // 新邮件高亮持续时间（淡出动画本身 250ms）
)

func reduced(c *ui.Context) bool { return c.Preferences().ReduceMotion }

// pressable：按下时轻微缩小到 0.97，松开后弹回。
func pressable(c *ui.Context, e ui.Element) ui.Element {
	if reduced(c) {
		return e
	}
	s := float32(1)
	if e.Pressed() {
		s = 0.97
	}
	return e.AnimatePose(ui.Pose{Scale: s}, ui.Animation{Duration: pressDur})
}

// colorFade：hover、选中时背景色平滑过渡。
func colorFade(c *ui.Context, e ui.Element) {
	if reduced(c) {
		return
	}
	e.Transition(ui.ElementTransition{Duration: colorDur, Colors: true})
}

// folderSwitch：切换文件夹或账户时，列表整体淡入，带一点向上的位移。
func folderSwitch(c *ui.Context, e ui.Element) {
	if reduced(c) {
		return
	}
	e.Transition(ui.ElementTransition{Duration: switchDur, Colors: true, Enter: &ui.Motion{Opacity: 0, Y: 4}})
}

// detailEnter：打开邮件时详情从右侧轻滑并淡入。
func detailEnter(c *ui.Context, e ui.Element) {
	if reduced(c) {
		return
	}
	e.Transition(ui.ElementTransition{Duration: detailDur, Colors: true, Enter: &ui.Motion{Opacity: 0, X: 12}})
}

// dialogEnter：对话框淡入，并从 0.96 放大到原尺寸。
func dialogEnter(c *ui.Context, e ui.Element) {
	if reduced(c) {
		return
	}
	e.Transition(ui.ElementTransition{Duration: dialogDur, Colors: true, Enter: &ui.Motion{Opacity: 0, Scale: 0.96}})
}

// arrivalHighlight：新到的邮件短暂显示强调色背景，然后淡回普通背景。
func (a *App) arrivalHighlight(c *ui.Context, e ui.Element, id string, on bool) {
	at, ok := a.fresh[id]
	if !ok {
		return
	}
	left := freshDur - a.now().Sub(at)
	if left <= 0 {
		delete(a.fresh, id)
		return
	}
	if reduced(c) {
		return // 不做高亮，静态界面里仍有未读加粗提示
	}
	if !on {
		e.Background(c.Theme().Accent.Alpha(0.14))
	}
	e.Transition(ui.ElementTransition{Duration: 250 * time.Millisecond, Colors: true})
	c.After(left) // 只在高亮结束时安排一帧，不需要轮询
}
