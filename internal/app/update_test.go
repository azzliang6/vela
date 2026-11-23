package app

import (
	"context"
	"errors"
	"testing"

	"github.com/egoist/mygo/ui"
)

type fakeUpdater struct {
	enabled    bool
	info       *UpdateInfo
	err        error
	installs   int
	relaunched bool
	progress   [][2]int64
	installErr error
}

func (f *fakeUpdater) Enabled() bool { return f.enabled }
func (f *fakeUpdater) Check(context.Context) (*UpdateInfo, error) {
	return f.info, f.err
}
func (f *fakeUpdater) Install(_ context.Context, progress func(done, total int64)) error {
	f.installs++
	if f.installErr != nil {
		return f.installErr
	}
	progress(50, 100)
	f.progress = append(f.progress, [2]int64{50, 100})
	progress(100, 100)
	f.progress = append(f.progress, [2]int64{100, 100})
	return nil
}
func (f *fakeUpdater) Relaunch() { f.relaunched = true }

// openSettingsForUpdate 打开设置并把「关于与更新」分组滚到可见处再返回测试器。
func openSettingsForUpdate(a *App) *ui.Tester {
	a.settingsOpen = true
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Scroll(640, 400, 0, 2000)
	tt.Frame()
	return tt
}

func TestUpdateCheckAndInstall(t *testing.T) {
	a, _ := newTestApp()
	f := &fakeUpdater{enabled: true, info: &UpdateInfo{Version: "v0.5.0", Notes: "新版本说明"}}
	a.SetUpdater(f)
	a.SetVersion("0.4.0")
	tt := openSettingsForUpdate(a)

	if !tt.HasText("v0.4.0") {
		t.Fatalf("设置里应显示当前版本：%q", tt.Texts())
	}
	if err := tt.Click("检查更新"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("发现新版本 v0.5.0") || !tt.HasText("新版本说明") {
		t.Fatalf("应显示新版本与说明：%q", tt.Texts())
	}

	tt.Scroll(640, 400, 0, 2000)
	tt.Frame()
	if err := tt.Click("下载并安装 v0.5.0"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if f.installs != 1 || !f.relaunched {
		t.Fatalf("应安装一次并重启：%d %v", f.installs, f.relaunched)
	}
	if len(f.progress) != 2 || !a.updState.done || a.updState.progress != 1 {
		t.Fatalf("进度应走完：%+v state=%+v", f.progress, a.updState)
	}
}

func TestUpdateUpToDate(t *testing.T) {
	a, _ := newTestApp()
	a.SetUpdater(&fakeUpdater{enabled: true}) // Check 返回 nil
	tt := openSettingsForUpdate(a)
	if err := tt.Click("检查更新"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("已是最新版本。") {
		t.Fatalf("应提示已是最新：%q", tt.Texts())
	}
}

func TestUpdateCheckError(t *testing.T) {
	a, _ := newTestApp()
	a.SetUpdater(&fakeUpdater{enabled: true, err: errors.New("网络不可用")})
	tt := openSettingsForUpdate(a)
	if err := tt.Click("检查更新"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("网络不可用") {
		t.Fatalf("应显示错误：%q", tt.Texts())
	}
}

func TestUpdateInstallErrorKeepsState(t *testing.T) {
	a, _ := newTestApp()
	f := &fakeUpdater{enabled: true, info: &UpdateInfo{Version: "v0.5.0"}, installErr: errors.New("下载失败")}
	a.SetUpdater(f)
	a.settingsOpen = true
	a.CheckUpdate()
	a.InstallUpdate()
	if f.relaunched || a.updState.err != "下载失败" || a.updState.done {
		t.Fatalf("安装失败应留在界面并显示错误、不重启：relaunch=%v state=%+v", f.relaunched, a.updState)
	}
}

func TestUpdateDisabled(t *testing.T) {
	a, _ := newTestApp()
	a.SetUpdater(&fakeUpdater{enabled: false})
	tt := openSettingsForUpdate(a)
	if !tt.HasText("不支持应用内更新") {
		t.Fatalf("应说明不能自行更新：%q", tt.Texts())
	}
	if tt.HasText("检查更新") {
		t.Fatal("不能自行更新时不应有检查按钮")
	}
}

func TestAutoCheckUpdateToasts(t *testing.T) {
	a, _ := newTestApp()
	a.SetUpdater(&fakeUpdater{enabled: true, info: &UpdateInfo{Version: "v0.5.0"}})
	a.AutoCheckUpdate()
	if a.updState.info == nil || len(a.toasts) != 1 {
		t.Fatalf("应发现新版本并提示：%+v %v", a.updState, a.toasts)
	}
}
