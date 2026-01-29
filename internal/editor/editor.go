package editor

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"unhexed/internal/buffer"
	"unhexed/internal/config"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type EditMode int

const (
	ModeNormal EditMode = iota
	ModeInsert
	ModeReplace
)

type View int

const (
	ViewMain View = iota
	ViewHelp
	ViewConfig
	ViewFind
	ViewGoto
	ViewOpen
	ViewSaveAs
	ViewConfirmQuit
	ViewConfirmClose
	ViewFileSavePrompt
	ViewFileChangedPrompt
)

type Tab struct {
	Buffer    *buffer.Buffer
	Cursor    int64
	ScrollY   int
	Selection struct {
		Active bool
		Start  int64
		End    int64
	}
}

type Model struct {
	tabs         []*Tab
	activeTab    int
	mode         EditMode
	view         View
	bigEndian    bool
	clipboard    []byte
	hexNibble    int // 0 or 1, for tracking hex input
	width        int
	height       int
	config       *config.Config
	styles       *config.Styles
	newFileCount int

	// Find dialog state
	findInput   string
	findMode    string // "ascii", "hex", "bits", "decimal"
	findWidth   int    // for decimal search
	findMatches int

	// Goto dialog state
	gotoInput string

	// File browser state
	browserPath  string
	browserItems []os.DirEntry
	browserIndex int
	browserFocus int // 0=list, 1=current tab btn, 2=new tab btn

	// Save As dialog state
	saveAsInput string

	// Config view state
	configIndex   int
	configInputs  map[string]string
	configChanged bool

	// Confirmation dialog
	confirmAction string

	// Error/status message
	statusMsg string
}

const bytesPerRow = 16

func NewModel(files []string) (*Model, error) {
	cfg, err := config.Load()
	if err != nil {
		cfg = config.DefaultConfig()
	}

	m := &Model{
		tabs:         make([]*Tab, 0),
		activeTab:    0,
		mode:         ModeNormal,
		view:         ViewMain,
		bigEndian:    true,
		config:       cfg,
		styles:       config.NewStyles(&cfg.Theme),
		findMode:     "ascii",
		findWidth:    1,
		configInputs: make(map[string]string),
	}

	// Load files or create new tab
	if len(files) == 0 {
		m.view = ViewOpen
		cwd, _ := os.Getwd()
		m.browserPath = cwd
		m.loadBrowserItems()
	} else {
		for _, f := range files {
			if err := m.openFile(f); err != nil {
				return nil, fmt.Errorf("failed to open %s: %w", f, err)
			}
		}
	}

	return m, nil
}

func (m *Model) openFile(filename string) error {
	buf, err := buffer.Open(filename)
	if err != nil {
		return err
	}
	m.tabs = append(m.tabs, &Tab{Buffer: buf})
	m.activeTab = len(m.tabs) - 1
	return nil
}

func (m *Model) newFile() {
	m.newFileCount++
	buf := buffer.New()
	m.tabs = append(m.tabs, &Tab{Buffer: buf})
	m.activeTab = len(m.tabs) - 1
}

func (m *Model) currentTab() *Tab {
	if len(m.tabs) == 0 || m.activeTab < 0 || m.activeTab >= len(m.tabs) {
		return nil
	}
	return m.tabs[m.activeTab]
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Clear status message on any key
	m.statusMsg = ""

	switch m.view {
	case ViewHelp:
		return m.handleHelpKey(msg)
	case ViewConfig:
		return m.handleConfigKey(msg)
	case ViewFind:
		return m.handleFindKey(msg)
	case ViewGoto:
		return m.handleGotoKey(msg)
	case ViewOpen:
		return m.handleOpenKey(msg)
	case ViewSaveAs:
		return m.handleSaveAsKey(msg)
	case ViewConfirmQuit:
		return m.handleConfirmQuitKey(msg)
	case ViewConfirmClose:
		return m.handleConfirmCloseKey(msg)
	case ViewFileSavePrompt:
		return m.handleFileSavePromptKey(msg)
	case ViewFileChangedPrompt:
		return m.handleFileChangedPromptKey(msg)
	default:
		return m.handleMainKey(msg)
	}
}

func (m *Model) handleMainKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	tab := m.currentTab()

	// Handle mode-specific input first
	if m.mode == ModeInsert || m.mode == ModeReplace {
		if msg.Type == tea.KeyEscape {
			m.mode = ModeNormal
			m.hexNibble = 0
			return m, nil
		}

		// Handle hex input
		if isHexChar(msg.String()) {
			return m.handleHexInput(msg.String())
		}
	}

	switch msg.String() {
	// Navigation
	case "up":
		m.moveCursor(-bytesPerRow, msg.Alt)
	case "down":
		m.moveCursor(bytesPerRow, msg.Alt)
	case "left":
		m.moveCursor(-1, msg.Alt)
	case "right":
		m.moveCursor(1, msg.Alt)
	case "shift+up":
		m.selectMove(-bytesPerRow)
	case "shift+down":
		m.selectMove(bytesPerRow)
	case "shift+left":
		m.selectMove(-1)
	case "shift+right":
		m.selectMove(1)
	case "pgup":
		m.moveCursor(-int64(m.visibleRows())*bytesPerRow, false)
	case "pgdown":
		m.moveCursor(int64(m.visibleRows())*bytesPerRow, false)
	case "home":
		if tab != nil {
			row := tab.Cursor / bytesPerRow
			m.setCursor(row * bytesPerRow)
		}
	case "end":
		if tab != nil {
			row := tab.Cursor / bytesPerRow
			m.setCursor(row*bytesPerRow + bytesPerRow - 1)
		}
	case "ctrl+home":
		m.setCursor(0)
	case "ctrl+end":
		if tab != nil && tab.Buffer.Size() > 0 {
			m.setCursor(tab.Buffer.Size() - 1)
		}

	// Commands
	case "q", "Q":
		return m.tryQuit()
	case "h", "H":
		m.view = ViewHelp
	case "c", "C":
		m.view = ViewConfig
		m.loadConfigInputs()
	case "o", "O":
		m.view = ViewOpen
		cwd, _ := os.Getwd()
		m.browserPath = cwd
		m.loadBrowserItems()
	case "s", "S", "ctrl+s":
		return m.trySave()
	case "a", "A":
		m.view = ViewSaveAs
		m.saveAsInput = ""
		if tab != nil && tab.Buffer.Filename() != "" {
			m.saveAsInput = tab.Buffer.Filename()
		}
	case "n", "N":
		m.newFile()
	case "i", "I":
		m.mode = ModeInsert
		m.hexNibble = 0
	case "r", "R":
		m.mode = ModeReplace
		m.hexNibble = 0
	case "f", "F":
		m.view = ViewFind
		m.findInput = ""
	case "g", "G":
		m.view = ViewGoto
		m.gotoInput = ""
	case "e", "E":
		m.bigEndian = !m.bigEndian
	case "tab":
		m.nextTab()
	case "shift+tab":
		m.prevTab()
	case "ctrl+w":
		return m.tryCloseTab()
	case "u", "U":
		if tab != nil && tab.Buffer.CanUndo() {
			tab.Buffer.Undo()
		}
	case "d", "D":
		if tab != nil && tab.Buffer.CanRedo() {
			tab.Buffer.Redo()
		}
	case "ctrl+x":
		m.cut()
	case "ctrl+c":
		m.copy()
	case "ctrl+v":
		m.paste()
	case "delete":
		m.delete(false)
	case "backspace":
		m.delete(true)
	}

	return m, nil
}

func (m *Model) handleHexInput(char string) (tea.Model, tea.Cmd) {
	tab := m.currentTab()
	if tab == nil {
		return m, nil
	}

	nibble := hexCharToNibble(char)

	if m.mode == ModeInsert {
		if m.hexNibble == 0 {
			// First nibble - insert a new byte
			tab.Buffer.Insert(tab.Cursor, []byte{nibble << 4})
			m.hexNibble = 1
		} else {
			// Second nibble - complete the byte
			if b, ok := tab.Buffer.GetByte(tab.Cursor); ok {
				tab.Buffer.Replace(tab.Cursor, (b&0xF0)|nibble)
			}
			m.hexNibble = 0
			tab.Cursor++
			if tab.Cursor > tab.Buffer.Size() {
				tab.Cursor = tab.Buffer.Size()
			}
		}
	} else if m.mode == ModeReplace {
		if tab.Cursor >= tab.Buffer.Size() {
			// At EOF, extend file
			tab.Buffer.Insert(tab.Buffer.Size(), []byte{nibble << 4})
			m.hexNibble = 1
		} else {
			if m.hexNibble == 0 {
				if b, ok := tab.Buffer.GetByte(tab.Cursor); ok {
					tab.Buffer.Replace(tab.Cursor, (nibble<<4)|(b&0x0F))
				}
				m.hexNibble = 1
			} else {
				if b, ok := tab.Buffer.GetByte(tab.Cursor); ok {
					tab.Buffer.Replace(tab.Cursor, (b&0xF0)|nibble)
				}
				m.hexNibble = 0
				tab.Cursor++
				if tab.Cursor >= tab.Buffer.Size() {
					tab.Cursor = tab.Buffer.Size() - 1
					if tab.Cursor < 0 {
						tab.Cursor = 0
					}
				}
			}
		}
	}

	m.clearSelection()
	return m, nil
}

func (m *Model) moveCursor(delta int64, clearSel bool) {
	tab := m.currentTab()
	if tab == nil {
		return
	}

	if clearSel || !tab.Selection.Active {
		m.clearSelection()
	}

	newPos := tab.Cursor + delta
	if newPos < 0 {
		newPos = 0
	}
	maxPos := tab.Buffer.Size() - 1
	if maxPos < 0 {
		maxPos = 0
	}
	if newPos > maxPos {
		newPos = maxPos
	}
	tab.Cursor = newPos
	m.ensureCursorVisible()
}

func (m *Model) setCursor(pos int64) {
	tab := m.currentTab()
	if tab == nil {
		return
	}

	m.clearSelection()
	if pos < 0 {
		pos = 0
	}
	maxPos := tab.Buffer.Size() - 1
	if maxPos < 0 {
		maxPos = 0
	}
	if pos > maxPos {
		pos = maxPos
	}
	tab.Cursor = pos
	m.ensureCursorVisible()
}

func (m *Model) selectMove(delta int64) {
	tab := m.currentTab()
	if tab == nil {
		return
	}

	if !tab.Selection.Active {
		tab.Selection.Active = true
		tab.Selection.Start = tab.Cursor
		tab.Selection.End = tab.Cursor
	}

	newPos := tab.Cursor + delta
	if newPos < 0 {
		newPos = 0
	}
	maxPos := tab.Buffer.Size() - 1
	if maxPos < 0 {
		maxPos = 0
	}
	if newPos > maxPos {
		newPos = maxPos
	}

	tab.Cursor = newPos
	tab.Selection.End = newPos
	m.ensureCursorVisible()
}

func (m *Model) clearSelection() {
	tab := m.currentTab()
	if tab != nil {
		tab.Selection.Active = false
	}
}

func (m *Model) getSelectedRange() (int64, int64) {
	tab := m.currentTab()
	if tab == nil || !tab.Selection.Active {
		return -1, -1
	}
	start, end := tab.Selection.Start, tab.Selection.End
	if start > end {
		start, end = end, start
	}
	return start, end
}

func (m *Model) ensureCursorVisible() {
	tab := m.currentTab()
	if tab == nil {
		return
	}

	visRows := m.visibleRows()
	cursorRow := int(tab.Cursor / bytesPerRow)

	if cursorRow < tab.ScrollY {
		tab.ScrollY = cursorRow
	} else if cursorRow >= tab.ScrollY+visRows {
		tab.ScrollY = cursorRow - visRows + 1
	}
}

func (m *Model) visibleRows() int {
	// Account for legend, tabs, column header, decoder panel
	rows := m.height - 10
	if rows < 1 {
		rows = 1
	}
	return rows
}

func (m *Model) nextTab() {
	if len(m.tabs) > 1 {
		m.activeTab = (m.activeTab + 1) % len(m.tabs)
	}
}

func (m *Model) prevTab() {
	if len(m.tabs) > 1 {
		m.activeTab = (m.activeTab - 1 + len(m.tabs)) % len(m.tabs)
	}
}

func (m *Model) copy() {
	tab := m.currentTab()
	if tab == nil {
		return
	}

	if tab.Selection.Active {
		start, end := m.getSelectedRange()
		m.clipboard = tab.Buffer.GetBytes(start, int(end-start+1))
	} else {
		if b, ok := tab.Buffer.GetByte(tab.Cursor); ok {
			m.clipboard = []byte{b}
		}
	}
}

func (m *Model) cut() {
	m.copy()
	m.delete(false)
}

func (m *Model) paste() {
	tab := m.currentTab()
	if tab == nil || len(m.clipboard) == 0 {
		return
	}

	if m.mode == ModeInsert {
		tab.Buffer.Insert(tab.Cursor, m.clipboard)
		tab.Cursor += int64(len(m.clipboard))
	} else {
		tab.Buffer.ReplaceBytes(tab.Cursor, m.clipboard)
	}
	m.clearSelection()
}

func (m *Model) delete(backspace bool) {
	tab := m.currentTab()
	if tab == nil || m.mode != ModeNormal {
		return
	}

	if tab.Selection.Active {
		start, end := m.getSelectedRange()
		tab.Buffer.Delete(start, int(end-start+1))
		tab.Cursor = start
		m.clearSelection()
	} else {
		if backspace {
			if tab.Cursor > 0 {
				tab.Buffer.Delete(tab.Cursor-1, 1)
				tab.Cursor--
			}
		} else {
			if tab.Cursor < tab.Buffer.Size() {
				tab.Buffer.Delete(tab.Cursor, 1)
			}
		}
	}

	// Adjust cursor if past end
	if tab.Cursor >= tab.Buffer.Size() && tab.Buffer.Size() > 0 {
		tab.Cursor = tab.Buffer.Size() - 1
	}
	if tab.Cursor < 0 {
		tab.Cursor = 0
	}
}

func (m *Model) tryQuit() (tea.Model, tea.Cmd) {
	for _, tab := range m.tabs {
		if tab.Buffer.IsModified() {
			m.view = ViewConfirmQuit
			return m, nil
		}
	}
	return m, tea.Quit
}

func (m *Model) trySave() (tea.Model, tea.Cmd) {
	tab := m.currentTab()
	if tab == nil {
		return m, nil
	}

	if tab.Buffer.IsNew() || tab.Buffer.Filename() == "" {
		m.view = ViewSaveAs
		m.saveAsInput = ""
		return m, nil
	}

	// Check if file changed on disk
	changed, err := tab.Buffer.HasChangedOnDisk()
	if err == nil && changed {
		m.view = ViewFileChangedPrompt
		return m, nil
	}

	if err := tab.Buffer.Save(); err != nil {
		m.statusMsg = fmt.Sprintf("Error saving: %v", err)
	} else {
		m.statusMsg = "File saved"
	}
	return m, nil
}

func (m *Model) tryCloseTab() (tea.Model, tea.Cmd) {
	tab := m.currentTab()
	if tab == nil {
		return m, nil
	}

	if tab.Buffer.IsModified() {
		m.view = ViewConfirmClose
		return m, nil
	}

	return m.closeCurrentTab()
}

func (m *Model) closeCurrentTab() (tea.Model, tea.Cmd) {
	if len(m.tabs) == 0 {
		return m, nil
	}

	m.tabs = append(m.tabs[:m.activeTab], m.tabs[m.activeTab+1:]...)
	if m.activeTab >= len(m.tabs) {
		m.activeTab = len(m.tabs) - 1
	}

	if len(m.tabs) == 0 {
		// Show file browser instead of quitting
		m.view = ViewOpen
		cwd, _ := os.Getwd()
		m.browserPath = cwd
		m.loadBrowserItems()
	}

	return m, nil
}

func (m *Model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEscape || msg.String() == "h" || msg.String() == "H" {
		m.view = ViewMain
	}
	return m, nil
}

func (m *Model) handleConfigKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		if m.configChanged {
			m.view = ViewFileSavePrompt
			m.confirmAction = "config"
		} else {
			m.view = ViewMain
		}
	case tea.KeyUp:
		if m.configIndex > 0 {
			m.configIndex--
		}
	case tea.KeyDown:
		m.configIndex++
	case tea.KeyBackspace:
		key := m.getConfigKey(m.configIndex)
		if key != "" && len(m.configInputs[key]) > 0 {
			m.configInputs[key] = m.configInputs[key][:len(m.configInputs[key])-1]
			m.configChanged = true
		}
	default:
		if len(msg.String()) == 1 {
			key := m.getConfigKey(m.configIndex)
			if key != "" {
				m.configInputs[key] += msg.String()
				m.configChanged = true
			}
		}
	}
	return m, nil
}

func (m *Model) getConfigKey(index int) string {
	keys := []string{
		"background", "marker_background", "marker_insert_background",
		"marker_replace_background", "index_marker_background", "legend_background",
		"legend_highlight", "border_color", "endian_color", "active_tab",
		"selection_background",
	}
	if index >= 0 && index < len(keys) {
		return keys[index]
	}
	return ""
}

func (m *Model) loadConfigInputs() {
	m.configInputs = map[string]string{
		"background":                m.config.Theme.Background,
		"marker_background":         m.config.Theme.MarkerBackground,
		"marker_insert_background":  m.config.Theme.MarkerInsertBackground,
		"marker_replace_background": m.config.Theme.MarkerReplaceBackground,
		"index_marker_background":   m.config.Theme.IndexMarkerBackground,
		"legend_background":         m.config.Theme.LegendBackground,
		"legend_highlight":          m.config.Theme.LegendHighlight,
		"border_color":              m.config.Theme.BorderColor,
		"endian_color":              m.config.Theme.EndianColor,
		"active_tab":                m.config.Theme.ActiveTab,
		"selection_background":      m.config.Theme.SelectionBackground,
	}
	m.configChanged = false
	m.configIndex = 0
}

func (m *Model) saveConfig() {
	m.config.Theme.Background = m.configInputs["background"]
	m.config.Theme.MarkerBackground = m.configInputs["marker_background"]
	m.config.Theme.MarkerInsertBackground = m.configInputs["marker_insert_background"]
	m.config.Theme.MarkerReplaceBackground = m.configInputs["marker_replace_background"]
	m.config.Theme.IndexMarkerBackground = m.configInputs["index_marker_background"]
	m.config.Theme.LegendBackground = m.configInputs["legend_background"]
	m.config.Theme.LegendHighlight = m.configInputs["legend_highlight"]
	m.config.Theme.BorderColor = m.configInputs["border_color"]
	m.config.Theme.EndianColor = m.configInputs["endian_color"]
	m.config.Theme.ActiveTab = m.configInputs["active_tab"]
	m.config.Theme.SelectionBackground = m.configInputs["selection_background"]
	m.config.Save()
	m.styles = config.NewStyles(&m.config.Theme)
}

func (m *Model) handleFindKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		m.view = ViewMain
	case tea.KeyUp:
		modes := []string{"ascii", "hex", "bits", "decimal"}
		for i, mode := range modes {
			if mode == m.findMode && i > 0 {
				m.findMode = modes[i-1]
				m.findInput = ""
				break
			}
		}
	case tea.KeyDown:
		modes := []string{"ascii", "hex", "bits", "decimal"}
		for i, mode := range modes {
			if mode == m.findMode && i < len(modes)-1 {
				m.findMode = modes[i+1]
				m.findInput = ""
				break
			}
		}
	case tea.KeyEnter:
		m.doFind(true)
	case tea.KeyBackspace:
		if len(m.findInput) > 0 {
			m.findInput = m.findInput[:len(m.findInput)-1]
			m.updateFindMatches()
		}
	default:
		char := msg.String()
		if m.isValidFindChar(char) {
			m.findInput += char
			m.updateFindMatches()
			m.doFind(true)
		}
	}
	return m, nil
}

func (m *Model) isValidFindChar(char string) bool {
	if len(char) != 1 {
		return false
	}
	switch m.findMode {
	case "hex":
		return isHexChar(char)
	case "bits":
		return char == "0" || char == "1"
	case "decimal":
		return char >= "0" && char <= "9"
	default:
		return true
	}
}

func (m *Model) getFindPattern() []byte {
	switch m.findMode {
	case "hex":
		// Convert hex string to bytes
		s := strings.ReplaceAll(m.findInput, " ", "")
		if len(s)%2 != 0 {
			s = "0" + s
		}
		result := make([]byte, len(s)/2)
		for i := 0; i < len(s); i += 2 {
			b, _ := strconv.ParseUint(s[i:i+2], 16, 8)
			result[i/2] = byte(b)
		}
		return result
	case "bits":
		// Convert bit string to bytes
		s := strings.ReplaceAll(m.findInput, " ", "")
		for len(s)%8 != 0 {
			s = "0" + s
		}
		result := make([]byte, len(s)/8)
		for i := 0; i < len(s); i += 8 {
			var b byte
			for j := 0; j < 8; j++ {
				if s[i+j] == '1' {
					b |= 1 << (7 - j)
				}
			}
			result[i/8] = b
		}
		return result
	case "decimal":
		// Convert decimal to bytes based on width
		n, _ := strconv.ParseUint(m.findInput, 10, 64)
		result := make([]byte, m.findWidth)
		for i := 0; i < m.findWidth; i++ {
			if m.bigEndian {
				result[m.findWidth-1-i] = byte(n >> (i * 8))
			} else {
				result[i] = byte(n >> (i * 8))
			}
		}
		return result
	default: // ascii
		return []byte(m.findInput)
	}
}

func (m *Model) updateFindMatches() {
	tab := m.currentTab()
	if tab == nil {
		m.findMatches = 0
		return
	}
	pattern := m.getFindPattern()
	m.findMatches = tab.Buffer.CountMatches(pattern)
}

func (m *Model) doFind(forward bool) {
	tab := m.currentTab()
	if tab == nil || m.findInput == "" {
		return
	}

	pattern := m.getFindPattern()
	start := tab.Cursor
	if forward {
		start++
	}
	pos := tab.Buffer.Find(pattern, start, forward)
	if pos >= 0 {
		tab.Cursor = pos
		m.ensureCursorVisible()
	}
}

func (m *Model) handleGotoKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		m.view = ViewMain
	case tea.KeyEnter:
		m.doGoto()
		m.view = ViewMain
	case tea.KeyBackspace:
		if len(m.gotoInput) > 0 {
			m.gotoInput = m.gotoInput[:len(m.gotoInput)-1]
		}
	default:
		char := msg.String()
		if len(char) == 1 && (isHexChar(char) || char == "x" || char == "X") {
			m.gotoInput += char
		}
	}
	return m, nil
}

func (m *Model) doGoto() {
	tab := m.currentTab()
	if tab == nil || m.gotoInput == "" {
		return
	}

	var offset int64
	input := strings.ToLower(m.gotoInput)
	if strings.HasPrefix(input, "0x") {
		offset, _ = strconv.ParseInt(input[2:], 16, 64)
	} else {
		offset, _ = strconv.ParseInt(input, 10, 64)
	}

	m.setCursor(offset)
}

func (m *Model) handleOpenKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		if len(m.tabs) > 0 {
			m.view = ViewMain
		}
	case tea.KeyUp:
		if m.browserFocus == 0 && m.browserIndex > 0 {
			m.browserIndex--
		}
	case tea.KeyDown:
		if m.browserFocus == 0 && m.browserIndex < len(m.browserItems)-1 {
			m.browserIndex++
		}
	case tea.KeyLeft:
		if m.browserFocus > 0 {
			m.browserFocus--
		}
	case tea.KeyRight:
		if m.browserFocus < 2 {
			m.browserFocus++
		}
	case tea.KeyTab:
		m.browserFocus = (m.browserFocus + 1) % 3
	case tea.KeyEnter:
		return m.handleBrowserEnter()
	}
	return m, nil
}

func (m *Model) handleBrowserEnter() (tea.Model, tea.Cmd) {
	if m.browserFocus == 0 {
		// File/directory selected
		if m.browserIndex < len(m.browserItems) {
			item := m.browserItems[m.browserIndex]
			path := filepath.Join(m.browserPath, item.Name())

			if item.IsDir() {
				m.browserPath = path
				m.loadBrowserItems()
				m.browserIndex = 0
			} else {
				// Open file in new tab
				if err := m.openFile(path); err != nil {
					m.statusMsg = fmt.Sprintf("Error: %v", err)
				} else {
					m.view = ViewMain
				}
			}
		}
	} else if m.browserFocus == 1 {
		// Open in current tab
		if m.browserIndex < len(m.browserItems) {
			item := m.browserItems[m.browserIndex]
			if !item.IsDir() {
				path := filepath.Join(m.browserPath, item.Name())
				buf, err := buffer.Open(path)
				if err != nil {
					m.statusMsg = fmt.Sprintf("Error: %v", err)
				} else {
					if len(m.tabs) == 0 {
						m.tabs = append(m.tabs, &Tab{Buffer: buf})
						m.activeTab = 0
					} else {
						m.tabs[m.activeTab] = &Tab{Buffer: buf}
					}
					m.view = ViewMain
				}
			}
		}
	} else {
		// Open in new tab
		if m.browserIndex < len(m.browserItems) {
			item := m.browserItems[m.browserIndex]
			if !item.IsDir() {
				path := filepath.Join(m.browserPath, item.Name())
				if err := m.openFile(path); err != nil {
					m.statusMsg = fmt.Sprintf("Error: %v", err)
				} else {
					m.view = ViewMain
				}
			}
		}
	}
	return m, nil
}

func (m *Model) loadBrowserItems() {
	entries, err := os.ReadDir(m.browserPath)
	if err != nil {
		m.browserItems = nil
		return
	}

	// Add parent directory
	m.browserItems = make([]os.DirEntry, 0, len(entries)+1)

	// Sort: directories first, then files
	var dirs, files []os.DirEntry
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name() < dirs[j].Name() })
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })

	// Add ".." for parent directory if not at root
	if m.browserPath != "/" {
		m.browserItems = append(m.browserItems, &parentDirEntry{})
	}
	m.browserItems = append(m.browserItems, dirs...)
	m.browserItems = append(m.browserItems, files...)
}

type parentDirEntry struct{}

func (p *parentDirEntry) Name() string               { return ".." }
func (p *parentDirEntry) IsDir() bool                { return true }
func (p *parentDirEntry) Type() os.FileMode          { return os.ModeDir }
func (p *parentDirEntry) Info() (os.FileInfo, error) { return nil, nil }

func (m *Model) handleSaveAsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		m.view = ViewMain
	case tea.KeyEnter:
		if m.saveAsInput != "" {
			tab := m.currentTab()
			if tab != nil {
				if err := tab.Buffer.SaveAs(m.saveAsInput); err != nil {
					m.statusMsg = fmt.Sprintf("Error: %v", err)
				} else {
					m.statusMsg = "File saved"
					m.view = ViewMain
				}
			}
		}
	case tea.KeyBackspace:
		if len(m.saveAsInput) > 0 {
			m.saveAsInput = m.saveAsInput[:len(m.saveAsInput)-1]
		}
	default:
		if len(msg.String()) == 1 || msg.String() == " " {
			m.saveAsInput += msg.String()
		}
	}
	return m, nil
}

func (m *Model) handleConfirmQuitKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		return m, tea.Quit
	case "n", "N", "escape":
		m.view = ViewMain
	}
	return m, nil
}

func (m *Model) handleConfirmCloseKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		tab := m.currentTab()
		if tab != nil {
			if tab.Buffer.IsNew() {
				m.view = ViewSaveAs
				m.saveAsInput = ""
			} else {
				tab.Buffer.Save()
				return m.closeCurrentTab()
			}
		}
	case "n", "N":
		return m.closeCurrentTab()
	case "escape":
		m.view = ViewMain
	}
	return m, nil
}

func (m *Model) handleFileSavePromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if m.confirmAction == "config" {
			m.saveConfig()
		}
		m.view = ViewMain
		m.confirmAction = ""
	case "n", "N":
		m.view = ViewMain
		m.confirmAction = ""
	case "escape":
		m.view = ViewConfig
		m.confirmAction = ""
	}
	return m, nil
}

func (m *Model) handleFileChangedPromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		tab := m.currentTab()
		if tab != nil {
			if err := tab.Buffer.Save(); err != nil {
				m.statusMsg = fmt.Sprintf("Error: %v", err)
			} else {
				m.statusMsg = "File saved"
			}
		}
		m.view = ViewMain
	case "n", "N", "escape":
		m.view = ViewMain
	}
	return m, nil
}

func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Legend
	b.WriteString(m.renderLegend())
	b.WriteString("\n")

	switch m.view {
	case ViewHelp:
		b.WriteString(m.renderHelp())
	case ViewConfig:
		b.WriteString(m.renderConfig())
	case ViewFind:
		b.WriteString(m.renderFind())
	case ViewGoto:
		b.WriteString(m.renderGoto())
	case ViewOpen:
		b.WriteString(m.renderOpen())
	case ViewSaveAs:
		b.WriteString(m.renderSaveAs())
	case ViewConfirmQuit:
		b.WriteString(m.renderMainView())
		b.WriteString("\n")
		b.WriteString(m.renderConfirmDialog("Unsaved changes. Quit anyway? (Y/N)"))
	case ViewConfirmClose:
		b.WriteString(m.renderMainView())
		b.WriteString("\n")
		b.WriteString(m.renderConfirmDialog("Save before closing? (Y)es/(N)o/E(sc)ape"))
	case ViewFileSavePrompt:
		b.WriteString(m.renderMainView())
		b.WriteString("\n")
		b.WriteString(m.renderConfirmDialog("Save changes? (Y/N)"))
	case ViewFileChangedPrompt:
		b.WriteString(m.renderMainView())
		b.WriteString("\n")
		b.WriteString(m.renderConfirmDialog("File changed on disk. Overwrite? (Y/N)"))
	default:
		b.WriteString(m.renderMainView())
	}

	// Status message
	if m.statusMsg != "" {
		b.WriteString("\n")
		b.WriteString(m.statusMsg)
	}

	return b.String()
}

func (m *Model) renderLegend() string {
	var items []string

	hl := func(text string, highlightIdx int) string {
		var result strings.Builder
		for i, ch := range text {
			if i == highlightIdx {
				result.WriteString(m.styles.LegendHighlight.Render(string(ch)))
			} else {
				result.WriteString(m.styles.Legend.Render(string(ch)))
			}
		}
		return result.String()
	}

	// Always visible
	items = append(items, hl("Quit", 0))
	items = append(items, hl("Help", 0))
	items = append(items, hl("Config", 0))

	if m.view == ViewMain {
		items = append(items, hl("Open", 0))
		items = append(items, hl("Save", 0))
		items = append(items, hl("sAve As", 1))
		items = append(items, hl("New", 0))
		items = append(items, hl("Insert", 0))
		items = append(items, hl("Replace", 0))
		items = append(items, hl("Find", 0))
		items = append(items, hl("Goto", 0))
		items = append(items, hl("Endian", 0))
		items = append(items, m.styles.LegendHighlight.Render("TAB"))

		tab := m.currentTab()
		if tab != nil {
			if tab.Buffer.CanUndo() {
				items = append(items, hl("Undo", 0))
			} else {
				items = append(items, m.styles.Disabled.Render("Undo"))
			}
			if tab.Buffer.CanRedo() {
				items = append(items, hl("reDo", 2))
			} else {
				items = append(items, m.styles.Disabled.Render("reDo"))
			}
		}

		items = append(items, m.styles.LegendHighlight.Render("^X")+" "+m.styles.LegendHighlight.Render("^C")+" "+m.styles.LegendHighlight.Render("^V"))
	} else if m.view == ViewFind || m.view == ViewGoto || m.view == ViewOpen || m.view == ViewSaveAs {
		items = append(items, m.styles.LegendHighlight.Render("ESC")+" Back")
	}

	legend := strings.Join(items, m.styles.Legend.Render(" | "))
	return m.styles.Legend.Width(m.width).Render(legend)
}

func (m *Model) renderMainView() string {
	var b strings.Builder

	// File tabs
	b.WriteString(m.renderTabs())
	b.WriteString("\n")

	if len(m.tabs) == 0 {
		b.WriteString("\nNo file open. Press O to open a file or N for new file.\n")
		return b.String()
	}

	tab := m.currentTab()
	if tab == nil {
		return b.String()
	}

	// Column header
	b.WriteString(m.renderColumnHeader())
	b.WriteString("\n")

	// Editor view
	b.WriteString(m.renderEditor())

	// Decoder panel
	b.WriteString("\n")
	b.WriteString(m.renderDecoder())

	return b.String()
}

func (m *Model) renderTabs() string {
	if len(m.tabs) == 0 {
		return ""
	}

	var tabs []string
	for i, tab := range m.tabs {
		name := tab.Buffer.Filename()
		if name == "" {
			name = "[New File]"
		} else {
			name = filepath.Base(name)
		}

		style := m.styles.InactiveTab
		if i == m.activeTab {
			style = m.styles.ActiveTab
		}
		if tab.Buffer.IsModified() {
			name = "*" + name
			if i != m.activeTab {
				style = m.styles.UnsavedFile
			}
		}

		tabs = append(tabs, style.Render(name))
	}

	return strings.Join(tabs, " | ")
}

func (m *Model) renderColumnHeader() string {
	tab := m.currentTab()
	if tab == nil {
		return ""
	}

	// Offset column width (8 hex chars)
	header := strings.Repeat(" ", 10)

	// Hex column headers
	cursorCol := int(tab.Cursor % bytesPerRow)
	for i := 0; i < bytesPerRow; i++ {
		hex := fmt.Sprintf("%02X", i)
		if i == cursorCol {
			hex = m.styles.IndexMarker.Render(hex)
		}
		header += hex
		if i < bytesPerRow-1 {
			if (i+1)%8 == 0 {
				header += "  "
			} else if (i+1)%4 == 0 {
				header += " "
			}
			header += " "
		}
	}

	return header
}

func (m *Model) renderEditor() string {
	tab := m.currentTab()
	if tab == nil {
		return ""
	}

	var lines []string
	visRows := m.visibleRows()
	startOffset := int64(tab.ScrollY) * bytesPerRow

	selStart, selEnd := m.getSelectedRange()

	for row := 0; row < visRows; row++ {
		rowOffset := startOffset + int64(row)*bytesPerRow
		if rowOffset >= tab.Buffer.Size() && rowOffset > 0 {
			break
		}

		// Offset column
		offsetStr := fmt.Sprintf("%08X  ", rowOffset)
		cursorRow := tab.Cursor / bytesPerRow
		if int64(tab.ScrollY+row) == cursorRow {
			offsetStr = m.styles.IndexMarker.Render(offsetStr)
		}

		// Hex and ASCII - build strings directly to match header alignment
		var hexLine strings.Builder
		var asciiLine strings.Builder

		for col := 0; col < bytesPerRow; col++ {
			offset := rowOffset + int64(col)
			b, ok := tab.Buffer.GetByte(offset)

			hexStr := "  "
			asciiStr := " "

			if ok {
				hexStr = fmt.Sprintf("%02X", b)
				if b >= 32 && b < 127 {
					asciiStr = string(b)
				} else {
					asciiStr = "."
				}
			}

			// Apply styling
			style := m.styles.Normal

			// Check if in selection
			if tab.Selection.Active && offset >= selStart && offset <= selEnd {
				style = m.styles.Selection
			} else if offset == tab.Cursor {
				// Cursor styling
				switch m.mode {
				case ModeInsert:
					style = m.styles.MarkerInsert
				case ModeReplace:
					style = m.styles.MarkerReplace
				default:
					style = m.styles.MarkerNormal
				}
			} else if ok {
				// Endian highlighting
				endianStart, endianEnd := m.getEndianRange(tab.Cursor)
				if offset >= endianStart && offset <= endianEnd && offset != tab.Cursor {
					style = m.styles.Endian
				}
			}

			hexLine.WriteString(style.Render(hexStr))
			asciiLine.WriteString(style.Render(asciiStr))

			// Spacing - must match renderColumnHeader exactly
			if col < bytesPerRow-1 {
				if (col+1)%8 == 0 {
					hexLine.WriteString("  ") // 2 extra spaces after byte 7
				} else if (col+1)%4 == 0 {
					hexLine.WriteString(" ") // 1 extra space after byte 3, 11
				}
				hexLine.WriteString(" ") // normal space between bytes
			}
		}

		line := offsetStr + hexLine.String() + "  " + asciiLine.String()
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func (m *Model) getEndianRange(cursor int64) (int64, int64) {
	if m.bigEndian {
		return cursor, cursor + 15
	}
	return cursor - 15, cursor
}

func (m *Model) renderDecoder() string {
	tab := m.currentTab()
	if tab == nil {
		return ""
	}

	var b strings.Builder

	endianStr := "Big"
	if !m.bigEndian {
		endianStr = "Little"
	}
	b.WriteString(m.styles.DecoderLabel.Render("Endianness: "))
	b.WriteString(m.styles.DecoderValue.Render(endianStr))
	b.WriteString("\n")

	// Get bytes for decoding
	bytes := m.getDecoderBytes(16)

	// Bit string (128 bits) - split into two rows of 64 bits each
	// First row: Bits (0-63) - bytes 0-7
	b.WriteString(m.styles.DecoderLabel.Render("Bits (0-63):   "))
	if len(bytes) > 0 {
		var bits strings.Builder
		for i := 0; i < 8 && i < len(bytes); i++ {
			if i > 0 {
				bits.WriteString(" ")
			}
			bits.WriteString(fmt.Sprintf("%08b", bytes[i]))
		}
		b.WriteString(m.styles.DecoderValue.Render(bits.String()))
	} else {
		b.WriteString("-")
	}
	b.WriteString("\n")

	// Second row: Bits (64-127) - bytes 8-15
	b.WriteString(m.styles.DecoderLabel.Render("Bits (64-127): "))
	if len(bytes) > 8 {
		var bits strings.Builder
		for i := 8; i < 16 && i < len(bytes); i++ {
			if i > 8 {
				bits.WriteString(" ")
			}
			bits.WriteString(fmt.Sprintf("%08b", bytes[i]))
		}
		b.WriteString(m.styles.DecoderValue.Render(bits.String()))
	} else {
		b.WriteString("-")
	}
	b.WriteString("\n")

	// Integer values (8-64 bit)
	vals := []struct {
		label  string
		size   int
		signed bool
	}{
		{"u8", 1, false}, {"i8", 1, true},
		{"u16", 2, false}, {"i16", 2, true},
		{"u32", 4, false}, {"i32", 4, true},
		{"u64", 8, false}, {"i64", 8, true},
	}

	for _, v := range vals {
		b.WriteString(m.styles.DecoderLabel.Render(fmt.Sprintf("%s: ", v.label)))
		if len(bytes) >= v.size {
			b.WriteString(m.styles.DecoderValue.Render(m.formatInt(bytes[:v.size], v.signed)))
		} else {
			b.WriteString("-")
		}
		b.WriteString("  ")
	}
	b.WriteString("\n")

	// 128-bit integers (separate row)
	b.WriteString(m.styles.DecoderLabel.Render("u128: "))
	if len(bytes) >= 16 {
		b.WriteString(m.styles.DecoderValue.Render(m.formatInt(bytes[:16], false)))
	} else {
		b.WriteString("-")
	}
	b.WriteString("  ")
	b.WriteString(m.styles.DecoderLabel.Render("i128: "))
	if len(bytes) >= 16 {
		b.WriteString(m.styles.DecoderValue.Render(m.formatInt(bytes[:16], true)))
	} else {
		b.WriteString("-")
	}
	b.WriteString("\n")

	// Float values
	b.WriteString(m.styles.DecoderLabel.Render("f32: "))
	if len(bytes) >= 4 {
		b.WriteString(m.styles.DecoderValue.Render(m.formatFloat32(bytes[:4])))
	} else {
		b.WriteString("-")
	}
	b.WriteString("  ")

	b.WriteString(m.styles.DecoderLabel.Render("f64: "))
	if len(bytes) >= 8 {
		b.WriteString(m.styles.DecoderValue.Render(m.formatFloat64(bytes[:8])))
	} else {
		b.WriteString("-")
	}

	return b.String()
}

func (m *Model) getDecoderBytes(count int) []byte {
	tab := m.currentTab()
	if tab == nil {
		return nil
	}

	if m.bigEndian {
		return tab.Buffer.GetBytes(tab.Cursor, count)
	}

	// Little endian - get bytes before cursor
	start := tab.Cursor - int64(count) + 1
	if start < 0 {
		start = 0
	}
	bytes := tab.Buffer.GetBytes(start, int(tab.Cursor-start+1))

	// Reverse for little endian interpretation
	result := make([]byte, len(bytes))
	for i, b := range bytes {
		result[len(bytes)-1-i] = b
	}
	return result
}

func (m *Model) formatInt(bytes []byte, signed bool) string {
	var order binary.ByteOrder = binary.BigEndian
	if !m.bigEndian {
		order = binary.LittleEndian
	}

	switch len(bytes) {
	case 1:
		if signed {
			return fmt.Sprintf("%d", int8(bytes[0]))
		}
		return fmt.Sprintf("%d", bytes[0])
	case 2:
		v := order.Uint16(bytes)
		if signed {
			return fmt.Sprintf("%d", int16(v))
		}
		return fmt.Sprintf("%d", v)
	case 4:
		v := order.Uint32(bytes)
		if signed {
			return fmt.Sprintf("%d", int32(v))
		}
		return fmt.Sprintf("%d", v)
	case 8:
		v := order.Uint64(bytes)
		if signed {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%d", v)
	case 16:
		// 128-bit integer
		var high, low uint64
		if m.bigEndian {
			high = binary.BigEndian.Uint64(bytes[:8])
			low = binary.BigEndian.Uint64(bytes[8:])
		} else {
			low = binary.LittleEndian.Uint64(bytes[:8])
			high = binary.LittleEndian.Uint64(bytes[8:])
		}

		n := new(big.Int)
		n.SetUint64(high)
		n.Lsh(n, 64)
		n.Or(n, new(big.Int).SetUint64(low))

		if signed && bytes[0]&0x80 != 0 {
			// Negative number - two's complement
			max := new(big.Int)
			max.Lsh(big.NewInt(1), 128)
			n.Sub(n, max)
		}
		return n.String()
	}
	return "-"
}

func (m *Model) formatFloat32(bytes []byte) string {
	var v uint32
	if m.bigEndian {
		v = binary.BigEndian.Uint32(bytes)
	} else {
		v = binary.LittleEndian.Uint32(bytes)
	}
	f := math.Float32frombits(v)
	if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
		return fmt.Sprintf("%v", f)
	}
	return fmt.Sprintf("%g", f)
}

func (m *Model) formatFloat64(bytes []byte) string {
	var v uint64
	if m.bigEndian {
		v = binary.BigEndian.Uint64(bytes)
	} else {
		v = binary.LittleEndian.Uint64(bytes)
	}
	f := math.Float64frombits(v)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Sprintf("%v", f)
	}
	return fmt.Sprintf("%g", f)
}

func (m *Model) renderHelp() string {
	help := `
HELP - Unhexed Hex Editor
========================

NAVIGATION
  Arrow keys      Move cursor
  Shift+Arrows    Select bytes
  PgUp/PgDown     Page up/down
  Home/End        Start/end of line
  Ctrl+Home/End   Start/end of file

FILE OPERATIONS
  O               Open file
  S / Ctrl+S      Save file
  A               Save As
  N               New file
  Ctrl+W          Close tab
  TAB             Next tab
  Shift+TAB       Previous tab

EDITING
  I               Enter Insert mode
  R               Enter Replace mode
  ESC             Exit Insert/Replace mode
  Ctrl+X          Cut
  Ctrl+C          Copy
  Ctrl+V          Paste
  Delete          Delete byte at cursor
  Backspace       Delete byte before cursor
  U               Undo
  D               Redo

OTHER
  F               Find
  G               Goto offset
  E               Toggle endianness
  H               Help (this screen)
  C               Configuration
  Q               Quit

Press ESC or H to close this help screen.
`
	return help
}

func (m *Model) renderConfig() string {
	var b strings.Builder
	b.WriteString("\nCONFIGURATION\n")
	b.WriteString("=============\n\n")
	b.WriteString("Theme Settings:\n\n")

	keys := []string{
		"background", "marker_background", "marker_insert_background",
		"marker_replace_background", "index_marker_background", "legend_background",
		"legend_highlight", "border_color", "endian_color", "active_tab",
		"selection_background",
	}

	labels := []string{
		"Background", "Marker Background", "Marker Insert Background",
		"Marker Replace Background", "Index Marker Background", "Legend Background",
		"Legend Highlight", "Border Color", "Endian Color", "Active Tab",
		"Selection Background",
	}

	for i, key := range keys {
		prefix := "  "
		if i == m.configIndex {
			prefix = "> "
		}
		value := m.configInputs[key]
		b.WriteString(fmt.Sprintf("%s%-27s: %s\n", prefix, labels[i], value))
	}

	b.WriteString("\nUse Up/Down to navigate, type to edit, ESC to exit\n")

	return b.String()
}

func (m *Model) renderFind() string {
	var b strings.Builder
	b.WriteString("\nFIND\n")
	b.WriteString("====\n\n")

	modes := []struct {
		key   string
		label string
	}{
		{"ascii", "ASCII"},
		{"hex", "Hex"},
		{"bits", "Bitstring"},
		{"decimal", "Decimal"},
	}

	for _, mode := range modes {
		prefix := "  "
		if mode.key == m.findMode {
			prefix = "> "
		}
		b.WriteString(fmt.Sprintf("%s%s: ", prefix, mode.label))
		if mode.key == m.findMode {
			b.WriteString(m.findInput)
			b.WriteString("_")
		}
		b.WriteString("\n")
	}

	b.WriteString(fmt.Sprintf("\nMatches: %d\n", m.findMatches))
	b.WriteString("\nPress Enter to find next, ESC to close\n")

	return b.String()
}

func (m *Model) renderGoto() string {
	var b strings.Builder
	b.WriteString("\nGOTO OFFSET\n")
	b.WriteString("===========\n\n")
	b.WriteString("Offset: ")
	b.WriteString(m.gotoInput)
	b.WriteString("_\n\n")
	b.WriteString("(Prefix with 0x for hex offset)\n")
	b.WriteString("\nPress Enter to go, ESC to close\n")

	return b.String()
}

func (m *Model) renderOpen() string {
	var b strings.Builder
	b.WriteString("\nOPEN FILE\n")
	b.WriteString("=========\n\n")
	b.WriteString("Path: ")
	b.WriteString(m.browserPath)
	b.WriteString("\n\n")

	// File list
	visibleItems := 15
	startIdx := 0
	if m.browserIndex >= visibleItems {
		startIdx = m.browserIndex - visibleItems + 1
	}

	for i := startIdx; i < len(m.browserItems) && i < startIdx+visibleItems; i++ {
		item := m.browserItems[i]
		prefix := "  "
		if i == m.browserIndex && m.browserFocus == 0 {
			prefix = "> "
		}
		name := item.Name()
		if item.IsDir() {
			name += "/"
		}
		b.WriteString(fmt.Sprintf("%s%s\n", prefix, name))
	}

	b.WriteString("\n")

	// Buttons
	btn1 := "[Open in current tab]"
	btn2 := "[Open in new tab]"
	if m.browserFocus == 1 {
		btn1 = ">" + btn1 + "<"
	}
	if m.browserFocus == 2 {
		btn2 = ">" + btn2 + "<"
	}
	b.WriteString(fmt.Sprintf("%s  %s\n", btn1, btn2))

	return b.String()
}

func (m *Model) renderSaveAs() string {
	var b strings.Builder
	b.WriteString("\nSAVE AS\n")
	b.WriteString("=======\n\n")
	b.WriteString("Filename: ")
	b.WriteString(m.saveAsInput)
	b.WriteString("_\n\n")
	b.WriteString("Press Enter to save, ESC to cancel\n")

	return b.String()
}

func (m *Model) renderConfirmDialog(message string) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.config.Theme.BorderColor)).
		Padding(1, 2).
		Render(message)
	return box
}

func isHexChar(s string) bool {
	if len(s) != 1 {
		return false
	}
	c := s[0]
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func hexCharToNibble(s string) byte {
	c := s[0]
	if c >= '0' && c <= '9' {
		return c - '0'
	}
	if c >= 'a' && c <= 'f' {
		return c - 'a' + 10
	}
	if c >= 'A' && c <= 'F' {
		return c - 'A' + 10
	}
	return 0
}
