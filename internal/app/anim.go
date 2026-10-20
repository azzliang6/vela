package app

import "github.com/egoist/mygo/ui"

// 动画在单独的提交里实现；这里先放空实现，保证界面先能工作。

func pressable(c *ui.Context, e ui.Element) ui.Element             { return e }
func colorFade(c *ui.Context, e ui.Element)                        {}
func folderSwitch(c *ui.Context, e ui.Element)                     {}
func detailEnter(c *ui.Context, e ui.Element)                      {}
func dialogEnter(c *ui.Context, e ui.Element)                      {}
func (a *App) arrivalHighlight(c *ui.Context, e ui.Element, id string, on bool) {}
