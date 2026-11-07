package tui

import "github.com/charmbracelet/bubbles/key"

// keyMap lists every key binding of the TUI. It also implements help.KeyMap so
// that the bottom line and the help overlay are rendered from the same source.
type keyMap struct {
	Submit      key.Binding
	ToggleFocus key.Binding
	UpDown      key.Binding
	PageScroll  key.Binding
	Clear       key.Binding
	Help        key.Binding
	Update      key.Binding
	Back        key.Binding
	Quit        key.Binding
}

// newKeyMap returns the frozen key bindings of the TUI.
func newKeyMap() keyMap {
	return keyMap{
		Submit:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "查询")),
		ToggleFocus: key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "切换")),
		UpDown:      key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "历史/滚动")),
		PageScroll:  key.NewBinding(key.WithKeys("pgup", "pgdown", "ctrl+u", "ctrl+d"), key.WithHelp("pgup/pgdn", "整页滚动")),
		Clear:       key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("ctrl+l", "清空结果")),
		Help:        key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "帮助")),
		Update:      key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "更新数据库")),
		Back:        key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "关闭/退出")),
		Quit:        key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "退出")),
	}
}

// ShortHelp is the single line hint shown below the input.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Submit, k.ToggleFocus, k.UpDown, k.Help, k.Update, k.Back}
}

// FullHelp is the complete key table shown in the help overlay.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Submit, k.ToggleFocus},
		{k.UpDown, k.PageScroll},
		{k.Clear, k.Help},
		{k.Update, k.Back, k.Quit},
	}
}
