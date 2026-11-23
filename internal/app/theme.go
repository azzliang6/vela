package app

import (
	"unicode"

	"github.com/egoist/mygo/ui"
)

// Vela 的「温润纸墨」色系：从左侧亚麻书案 → 中栏棉纸列表 → 右侧象牙信笺层层递进。
func velaTheme(dark bool) *ui.Theme {
	if dark {
		t := ui.DarkTheme()
		t.Background, t.Surface = ui.Hex("#141210"), ui.Hex("#1d1915")
		t.SurfaceHover, t.SurfacePressed = ui.Hex("#26211b"), ui.Hex("#2e2821")
		t.Border, t.Text, t.TextMuted = ui.Hex("#2c261f"), ui.Hex("#eae3d4"), ui.Hex("#8e8474")
		t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = ui.Hex("#f2611d"), ui.Hex("#ff7838"), ui.Hex("#d9480f"), ui.Hex("#1c0d05")
		t.Danger, t.Warning, t.Success = ui.Hex("#e06c6c"), ui.Hex("#e0a24c"), ui.Hex("#6ab98d")
		t.Selection, t.Focus = ui.RGBA(242, 97, 29, 0.24), ui.RGBA(242, 97, 29, 0.58)
		t.Scrollbar = ui.Hex("#756c5e")
		t.Radius = 10
		return t
	}
	t := ui.LightTheme()
	t.Background, t.Surface = ui.Hex("#f7f4ea"), ui.Hex("#fdfbf5")
	t.SurfaceHover, t.SurfacePressed = ui.Hex("#efe9da"), ui.Hex("#e5dfcd")
	t.Border, t.Text, t.TextMuted = ui.Hex("#e3dcc9"), ui.Hex("#1d1a15"), ui.Hex("#857b6b")
	t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = ui.Hex("#d9480f"), ui.Hex("#c2410c"), ui.Hex("#b23a08"), ui.Hex("#fff8f2")
	t.Danger, t.Warning, t.Success = ui.Hex("#b83232"), ui.Hex("#d97706"), ui.Hex("#2f7a4e")
	t.Selection, t.Focus = ui.RGBA(217, 72, 15, 0.14), ui.RGBA(217, 72, 15, 0.50)
	t.Scrollbar = ui.Hex("#8d8270")
	t.Radius = 10
	return t
}

// palette 补充三栏层次、卡片、徽章与弹窗的细分纸墨色令牌。
type palette struct {
	RailBg             ui.Color // 左侧边栏：沉稳亚麻底
	ListBg             ui.Color // 邮件列表栏：柔和棉纸底
	CardBg             ui.Color // 浮起纸张（搜索框、卡片、弹窗底）
	CardSelected       ui.Color // 选中的邮件卡片
	CardSelectedBorder ui.Color // 选中邮件卡片的暖边
	NavActiveBg        ui.Color // 侧栏选中胶囊底
	NavActiveBorder    ui.Color // 侧栏选中胶囊边框
	HoverWash          ui.Color // 悬停淡墨水洗色
	PressWash          ui.Color // 按下淡墨水洗色
	PaneDivider        ui.Color // 三栏主分割线
	SubtleDivider      ui.Color // 栏内柔和细线
	DialogBackdrop     ui.Color // 弹窗遮罩
	DialogBorder       ui.Color // 弹窗外框
	FieldGroupBg       ui.Color // 弹窗内分组表单底色
	TextSoft           ui.Color // 次级正文色（介于主色与 Muted 之间）
	AccentSoft         ui.Color // 强调色柔光底（AI 卡片、未读徽章）
	AccentBorder       ui.Color // 强调色描边
	KeycapBg           ui.Color // 快捷键键帽底色
}

func velaPalette(dark bool) palette {
	if dark {
		return palette{
			RailBg:             ui.Hex("#0f0d0b"),
			ListBg:             ui.Hex("#12100e"),
			CardBg:             ui.Hex("#1c1915"),
			CardSelected:       ui.Hex("#241e18"),
			CardSelectedBorder: ui.Hex("#473324"),
			NavActiveBg:        ui.Hex("#211c17"),
			NavActiveBorder:    ui.Hex("#332b22"),
			HoverWash:          ui.RGBA(255, 244, 228, 0.055),
			PressWash:          ui.RGBA(255, 244, 228, 0.095),
			PaneDivider:        ui.Hex("#241f1a"),
			SubtleDivider:      ui.Hex("#1f1b16"),
			DialogBackdrop:     ui.RGBA(4, 3, 2, 0.66),
			DialogBorder:       ui.Hex("#322b23"),
			FieldGroupBg:       ui.Hex("#161310"),
			TextSoft:           ui.Hex("#c4baa8"),
			AccentSoft:         ui.RGBA(242, 97, 29, 0.14),
			AccentBorder:       ui.RGBA(242, 97, 29, 0.30),
			KeycapBg:           ui.Hex("#25201a"),
		}
	}
	return palette{
		RailBg:             ui.Hex("#ede7d8"),
		ListBg:             ui.Hex("#f3efe4"),
		CardBg:             ui.Hex("#fdfbf5"),
		CardSelected:       ui.Hex("#fffef9"),
		CardSelectedBorder: ui.Hex("#e3c8b4"),
		NavActiveBg:        ui.Hex("#fdfbf5"),
		NavActiveBorder:    ui.Hex("#ded6c2"),
		HoverWash:          ui.RGBA(29, 26, 21, 0.048),
		PressWash:          ui.RGBA(29, 26, 21, 0.085),
		PaneDivider:        ui.Hex("#dfd8c5"),
		SubtleDivider:      ui.Hex("#e7e0ce"),
		DialogBackdrop:     ui.RGBA(24, 19, 14, 0.40),
		DialogBorder:       ui.Hex("#ddd5c1"),
		FieldGroupBg:       ui.Hex("#f6f2e7"),
		TextSoft:           ui.Hex("#4d463a"),
		AccentSoft:         ui.RGBA(217, 72, 15, 0.09),
		AccentBorder:       ui.RGBA(217, 72, 15, 0.22),
		KeycapBg:           ui.Hex("#e7e0ce"),
	}
}

var starColor = map[bool]ui.Color{false: ui.Hex("#d98a00"), true: ui.Hex("#f5be41")}

// categoryColor 为智能分类分配温和的矿物色点，避免侧栏全是重复的同色图标。
func categoryColor(id string, dark bool) ui.Color {
	if dark {
		switch id {
		case "important":
			return ui.Hex("#f2611d")
		case "finance":
			return ui.Hex("#e5a445")
		case "travel":
			return ui.Hex("#4fb3ad")
		case "social":
			return ui.Hex("#d97293")
		case "newsletter":
			return ui.Hex("#73b888")
		case "notification":
			return ui.Hex("#749ad6")
		default:
			return ui.Hex("#8e8474")
		}
	}
	switch id {
	case "important":
		return ui.Hex("#d9480f")
	case "finance":
		return ui.Hex("#c47612")
	case "travel":
		return ui.Hex("#267873")
	case "social":
		return ui.Hex("#ad4668")
	case "newsletter":
		return ui.Hex("#3a7a50")
	case "notification":
		return ui.Hex("#3e649c")
	default:
		return ui.Hex("#857b6b")
	}
}

// avatarTone 按发件人标识生成一组与纸墨主题协调的柔和矿物色（底色 + 字色）。
func avatarTone(seed string, dark bool) (bg, fg ui.Color) {
	var h uint32 = 2166136261
	for i := 0; i < len(seed); i++ {
		h = (h ^ uint32(seed[i])) * 16777619
	}
	if dark {
		tones := [][2]ui.Color{
			{ui.Hex("#332018"), ui.Hex("#f58d5c")}, // 赤陶
			{ui.Hex("#1d2b24"), ui.Hex("#7ec498")}, // 青苔
			{ui.Hex("#1e2633"), ui.Hex("#88ace0")}, // 黛蓝
			{ui.Hex("#302617"), ui.Hex("#ebb35e")}, // 赭石
			{ui.Hex("#2e1e26"), ui.Hex("#e08aa6")}, // 绛梅
			{ui.Hex("#1b2b2b"), ui.Hex("#6ec4be")}, // 矿物青
		}
		pair := tones[h%uint32(len(tones))]
		return pair[0], pair[1]
	}
	tones := [][2]ui.Color{
		{ui.Hex("#f7e3d9"), ui.Hex("#b83e0e")}, // 赤陶
		{ui.Hex("#dfece4"), ui.Hex("#2b6a42")}, // 青苔
		{ui.Hex("#e0e7f2"), ui.Hex("#335587")}, // 黛蓝
		{ui.Hex("#f5ead4"), ui.Hex("#9c6210")}, // 赭石
		{ui.Hex("#f2e1e7"), ui.Hex("#963858")}, // 绛梅
		{ui.Hex("#dceceb"), ui.Hex("#206662")}, // 矿物青
	}
	pair := tones[h%uint32(len(tones))]
	return pair[0], pair[1]
}

// senderInitial 提取发件人头像字符：中文取首字，西文取首字母大写。
func senderInitial(name, email string) string {
	src := name
	if src == "" {
		src = email
	}
	for _, r := range src {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return string(unicode.ToUpper(r))
		}
	}
	return "✉"
}
