package app

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/azzliang6/vela/internal/core"
	"github.com/egoist/mygo/ui"
)

// 备份与还原：把邮箱账户（含加密授权码）、同步游标、本地邮件和偏好设置
// 打包成一个口令加密的 .velabak 文件，方便换机迁移与数据备份。
// 真正的打包/解包在 internal/core（core.Backend.ExportBackup / ImportBackup），
// 这里只负责界面流程：密码表单、文件对话框选择路径、后台执行与结果提示。

// backupCapable 是后端可选的备份能力；生产环境 *core.Backend 实现。
type backupCapable interface {
	ExportBackup(passphrase string, settings json.RawMessage) ([]byte, error)
	ImportBackup(passphrase string, data []byte) (*core.RestoreResult, error)
}

func (a *App) backuper() backupCapable {
	b, _ := a.be.(backupCapable)
	return b
}

const backupExt = ".velabak"

func (a *App) StartBackupExport() { a.startBackup("export") }
func (a *App) StartBackupImport() { a.startBackup("import") }

func (a *App) startBackup(mode string) {
	if a.backuper() == nil {
		a.toast("当前后端不支持备份还原")
		return
	}
	a.backupMode = mode
	a.backupPass, a.backupPass2, a.backupPath, a.backupErr = "", "", "", ""
	a.backupOpen = true
}

// pickBackupFile 弹出系统对话框选择备份文件。
func (a *App) pickBackupFile() {
	if a.dialogs.OpenFile == nil {
		return
	}
	if p, ok := a.dialogs.OpenFile("选择 Vela 备份文件"); ok && p != "" {
		a.backupPath = p
	}
}

// SubmitBackup 处理备份弹窗的主按钮：导出（选路径 → 加密 → 落盘）
// 或还原（读文件 → 解密 → 覆盖本地数据 → 刷新界面）。
func (a *App) SubmitBackup() {
	bu := a.backuper()
	if bu == nil || a.backupBusy {
		return
	}
	a.backupErr = ""
	pass := a.backupPass
	if strings.TrimSpace(pass) == "" {
		a.backupErr = "请输入备份密码"
		return
	}

	if a.backupMode == "export" {
		if pass != a.backupPass2 {
			a.backupErr = "两次输入的密码不一致"
			return
		}
		path := strings.TrimSpace(a.backupPath)
		if a.dialogs.SaveFile != nil {
			name := "Vela备份-" + a.now().Format("20060102-150405") + backupExt
			p, ok := a.dialogs.SaveFile("选择备份文件的保存位置", name)
			if !ok || p == "" {
				return // 用户取消了保存对话框
			}
			path = p
		}
		if path == "" {
			a.backupErr = "请指定备份文件的保存路径"
			return
		}
		if !strings.EqualFold(filepath_Ext(path), backupExt) {
			path += backupExt
		}
		settings, _ := json.Marshal(a.settings)
		a.backupBusy = true
		a.run(func() func() {
			data, err := bu.ExportBackup(pass, settings)
			if err == nil {
				err = os.WriteFile(path, data, 0o600)
			}
			return func() {
				a.backupBusy = false
				if err != nil {
					a.backupErr = err.Error()
					return
				}
				a.backupOpen = false
				a.toast("备份已保存：" + path)
			}
		})
		return
	}

	// 还原
	path := strings.TrimSpace(a.backupPath)
	if path == "" {
		a.backupErr = "请选择备份文件"
		return
	}
	a.backupBusy = true
	a.run(func() func() {
		data, err := os.ReadFile(path)
		var res *core.RestoreResult
		if err == nil {
			res, err = bu.ImportBackup(pass, data)
		}
		return func() { a.afterRestore(res, err) }
	})
}

// afterRestore 在主线程应用还原结果：套用偏好、让旧的同步结果作废、刷新账户与邮件。
func (a *App) afterRestore(res *core.RestoreResult, err error) {
	a.backupBusy = false
	if err != nil {
		a.backupErr = err.Error()
		return
	}
	if len(res.Settings) > 0 {
		var s Settings
		if json.Unmarshal(res.Settings, &s) == nil {
			a.settings = s
		}
	}
	a.saveSettings()
	a.emitTheme()

	// 本机数据已经被整体换掉：还原之前发起的同步针对的是旧数据，结果不能再合并回来
	a.dataGen++
	a.accounts, _ = a.be.ListAccounts()
	a.emails, _ = a.be.GetEmails()
	a.selected = ""
	if !slices.ContainsFunc(a.accounts, func(x core.MailboxAccount) bool { return x.ID == a.account }) {
		a.account = "" // 正在筛选的账户已被覆盖掉：回到「全部账户」，否则列表会是空的
	}

	a.backupOpen = false
	msg := fmt.Sprintf("还原完成：%d 个邮箱账户、%d 封邮件", res.Accounts, res.Emails)
	if res.Snapshot != "" {
		msg += "（还原前的数据已留副本）"
	}
	a.toast(msg)
}

// filepath_Ext 返回小写的路径扩展名（含点）。
func filepath_Ext(path string) string {
	i := strings.LastIndexByte(path, '.')
	if i < 0 || strings.LastIndexByte(path, '/') > i || strings.LastIndexByte(path, '\\') > i {
		return ""
	}
	return path[i:]
}

// backupView 渲染备份导出 / 还原弹窗。
func (a *App) backupView(c *ui.Context, p palette) {
	t := c.Theme()
	export := a.backupMode == "export"
	title := "从备份还原"
	if export {
		title = "导出备份"
	}
	ui.Column(c).AlignItems(ui.Stretch).Children(func() {
		modalHeader(c, p, "archive", title, "", func() { a.backupOpen = false })

		ui.Column(c).Padding(2, 24, 4).Gap(12).AlignItems(ui.Stretch).Children(func() {
			note := fmt.Sprintf("还原会覆盖当前的邮箱账户、本地邮件与偏好设置，需要输入导出时设置的密码。覆盖之前会自动在本机留一份数据副本（保留最近 %d 份），在数据目录的 snapshots 文件夹里。", core.SnapshotKeep)
			if export {
				note = "备份会把邮箱账户（含授权码）、同步游标、本地邮件与偏好设置打包成一个密码加密的文件。请妥善保管文件与密码——没有密码将无法还原。"
			}
			ui.Row(c).Gap(8).AlignItems(ui.Start).Padding(10, 12).Radius(10).
				Background(p.FieldGroupBg).Border(1, p.SubtleDivider).Children(func() {
				ui.Icon(c, icon("shield")).FontSize(14).Shrink(0).TextColor(t.Accent).Margin(2, 0, 0, 0)
				ui.Text(c, note).FontSize(12).LineHeight(1.5).TextColor(t.TextMuted).Grow(1)
			})

			if export {
				ui.Column(c).Gap(4).AlignItems(ui.Stretch).Children(func() {
					ui.Text(c, "备份密码").FontSize(12.5).FontWeight(500).TextColor(p.TextSoft)
					ui.TextInput(c, &a.backupPass).Password().Placeholder("至少 4 个字符").Label("备份密码")
					ui.TextInput(c, &a.backupPass2).Password().Placeholder("再输入一次确认").Label("确认密码")
				})
			} else {
				ui.Column(c).Gap(4).AlignItems(ui.Stretch).Children(func() {
					ui.Text(c, "备份文件").FontSize(12.5).FontWeight(500).TextColor(p.TextSoft)
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.TextInput(c, &a.backupPath).Grow(1).Placeholder("选择或输入 " + backupExt + " 文件路径").Label("备份文件")
						if a.dialogs.OpenFile != nil {
							pressable(c, ui.Button(c, "浏览…")).OnClick(a.pickBackupFile)
						}
					})
					ui.TextInput(c, &a.backupPass).Password().Placeholder("导出时设置的备份密码").Label("备份密码")
				})
			}

			if a.backupErr != "" {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(10, 12).Radius(10).
					Background(t.Danger.Alpha(0.10)).Border(1, t.Danger.Alpha(0.28)).Children(func() {
					ui.Text(c, a.backupErr).FontSize(12.5).TextColor(t.Danger).Grow(1).Selectable()
				})
			}
		})

		ui.Row(c).Gap(8).Justify(ui.End).AlignItems(ui.Center).Padding(16, 24, 20).Children(func() {
			pressable(c, ui.Button(c, "取消")).OnClick(func() { a.backupOpen = false })
			label := "还原并覆盖"
			if export {
				label = "加密并导出…"
			}
			if a.backupBusy {
				label = "处理中…"
			}
			pressable(c, ui.PrimaryButton(c, label).Disabled(a.backupBusy)).OnClick(a.SubmitBackup)
		})
	})
}
