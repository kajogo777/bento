package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
)

// ── Styles ──────────────────────────────────────────────────────────

var (
	appStyle = lipgloss.NewStyle().Padding(0, 1)
	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	addStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	remStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	modStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	hdrStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("63"))
	scrStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	sepStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
)

// ── Messages ────────────────────────────────────────────────────────

type errMsg struct{ err error }
type checkpointsLoadedMsg struct{ summaries []CheckpointSummary }
type filesLoadedMsg struct {
	tag   string
	items []list.Item
}
type previewMsg struct {
	path    string
	content string
	isDiff  bool
}

// ── List items ──────────────────────────────────────────────────────

type cpItem struct{ s CheckpointSummary }

func (i cpItem) Title() string {
	tags := strings.Join(i.s.Tags, ", ")
	if i.s.Message != "" {
		return tags + "  " + dimStyle.Render(i.s.Message)
	}
	return tags
}
func (i cpItem) Description() string { return dimStyle.Render(i.s.Created) }
func (i cpItem) FilterValue() string {
	return strings.Join(i.s.Tags, " ") + " " + i.s.Message
}

type fileItem struct {
	entry      FileEntry
	layerName  string
	tag        string
	layerIndex int
}

func (i fileItem) Title() string {
	sigil := "  "
	switch i.entry.DiffStatus {
	case Added:
		sigil = addStyle.Render("+ ")
	case Removed:
		sigil = remStyle.Render("- ")
	case Modified:
		sigil = modStyle.Render("~ ")
	}
	scrub := ""
	if i.entry.HasScrubs {
		scrub = scrStyle.Render(" [S]")
	}
	layer := dimStyle.Render(i.layerName+":") + " "
	return sigil + layer + i.entry.Path + scrub
}
func (i fileItem) Description() string { return "" }
func (i fileItem) FilterValue() string { return i.entry.Path }

// ── Model ───────────────────────────────────────────────────────────

type viewState int

const (
	stateCheckpoints viewState = iota
	stateFiles
)

type Model struct {
	source ArtifactSource
	state  viewState

	cpList   list.Model
	fileList list.Model
	preview  viewport.Model

	currentTag   string
	allFileItems []list.Item

	filterMode bool
	filterText string

	previewTitle  string
	previewIsDiff bool

	width, height int
	errText       string
}

func NewModel(source ArtifactSource, _ string) Model {
	d := list.NewDefaultDelegate()
	d.ShowDescription = true
	d.SetSpacing(0)
	cpList := list.New(nil, d, 0, 0)
	cpList.Title = "Checkpoints"
	cpList.SetShowHelp(true)
	cpList.SetStatusBarItemName("checkpoint", "checkpoints")
	cpList.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		}
	}

	return Model{
		source:        source,
		state:         stateCheckpoints,
		cpList:        cpList,
		preview:       viewport.New(),
		previewIsDiff: true,
	}
}

func (m Model) Init() tea.Cmd {
	return func() tea.Msg {
		summaries, err := m.source.ListCheckpoints()
		if err != nil {
			return errMsg{err}
		}
		return checkpointsLoadedMsg{summaries}
	}
}

// Update is the main bubbletea update loop.
// IMPORTANT: bubbletea passes Model by value. All mutations happen on the
// local copy `m` which is returned. This is correct — no pointer receivers needed.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.sizeCheckpoints()
		if m.state == stateFiles {
			m.sizeFiles()
		}
		return m, nil

	case errMsg:
		m.errText = msg.err.Error()
		return m, nil

	case checkpointsLoadedMsg:
		items := make([]list.Item, len(msg.summaries))
		for i, s := range msg.summaries {
			items[i] = cpItem{s}
		}
		m.cpList.SetItems(items)
		return m, nil

	case filesLoadedMsg:
		m.allFileItems = msg.items
		m.rebuildFileList()
		return m, m.previewSelected()

	case previewMsg:
		m.previewTitle = msg.path
		m.previewIsDiff = msg.isDiff
		m.preview.SetContent(colorizeDiff(msg.content, msg.isDiff))
		m.preview.GotoTop()
		m.errText = ""
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		}

		switch m.state {
		case stateCheckpoints:
			return m.handleCheckpointKey(msg)
		case stateFiles:
			return m.handleFileKey(msg)
		}
	}

	return m, nil
}

func (m Model) handleCheckpointKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	if msg.String() == "enter" && m.cpList.FilterState() != list.Filtering {
		item, ok := m.cpList.SelectedItem().(cpItem)
		if ok && len(item.s.Tags) > 0 {
			return m.enterFilesState(item.s.Tags[0])
		}
	}

	if key.Matches(msg, m.cpList.KeyMap.Quit) {
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.cpList, cmd = m.cpList.Update(msg)
	return m, cmd
}

func (m Model) enterFilesState(tag string) (Model, tea.Cmd) {
	m.state = stateFiles
	m.currentTag = tag
	m.errText = ""
	m.previewTitle = ""
	m.filterMode = false
	m.filterText = ""
	m.preview.SetContent("")

	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	d.SetSpacing(0)
	m.fileList = list.New(nil, d, 0, 0)
	m.fileList.Title = tag
	m.fileList.SetShowHelp(true)
	m.fileList.SetFilteringEnabled(false)
	m.fileList.SetStatusBarItemName("file", "files")
	m.fileList.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
			key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "diff/raw")),
			key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
			key.NewBinding(key.WithKeys("shift+down"), key.WithHelp("S-dn/up", "scroll preview")),
		}
	}
	m.sizeFiles()

	return m, m.loadFiles(tag)
}

func (m Model) handleFileKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	// ── Filter input mode ──
	if m.filterMode {
		switch msg.String() {
		case "esc":
			m.filterMode = false
			m.filterText = ""
			m.rebuildFileList()
			return m, nil
		case "enter":
			m.filterMode = false
			return m, nil
		case "backspace":
			if len(m.filterText) > 0 {
				m.filterText = m.filterText[:len(m.filterText)-1]
				m.rebuildFileList()
			}
			return m, m.previewSelected()
		case "ctrl+c":
			return m, tea.Quit
		default:
			if r := msg.String(); len(r) == 1 {
				m.filterText += r
				m.rebuildFileList()
				return m, m.previewSelected()
			}
			return m, nil
		}
	}

	// ── Normal file browsing ──
	switch msg.String() {
	case "esc":
		if m.filterText != "" {
			m.filterText = ""
			m.rebuildFileList()
			return m, nil
		}
		m.state = stateCheckpoints
		m.previewTitle = ""
		return m, nil

	case "/":
		m.filterMode = true
		return m, nil

	case "tab":
		if m.previewTitle != "" {
			m.previewIsDiff = !m.previewIsDiff
			return m, m.previewSelected()
		}
		return m, nil

	case "q":
		return m, tea.Quit

	case "shift+down", "shift+j", "ctrl+d":
		m.preview.ScrollDown(5)
		return m, nil

	case "shift+up", "shift+k", "ctrl+u":
		m.preview.ScrollUp(5)
		return m, nil
	}

	// ── Pass remaining keys to list for up/down/pgup/pgdn ──
	prev := m.fileList.Index()
	var cmd tea.Cmd
	m.fileList, cmd = m.fileList.Update(msg)

	var previewCmd tea.Cmd
	if m.fileList.Index() != prev {
		previewCmd = m.previewSelected()
	}

	return m, tea.Batch(cmd, previewCmd)
}

// rebuildFileList applies the current filter and resets the list items.
// Must be called on the local Model copy (value receiver is fine since
// the caller returns this m).
func (m *Model) rebuildFileList() {
	if m.filterText == "" {
		m.fileList.SetItems(m.allFileItems)
	} else {
		filter := strings.ToLower(m.filterText)
		var filtered []list.Item
		for _, item := range m.allFileItems {
			if strings.Contains(strings.ToLower(item.FilterValue()), filter) {
				filtered = append(filtered, item)
			}
		}
		m.fileList.SetItems(filtered)
	}
	// Re-apply size after changing items (fixes pagination)
	m.sizeFiles()
}

// ── Sizing ──────────────────────────────────────────────────────────
// Separated from layout so they can be called independently.

func (m *Model) sizeCheckpoints() {
	if m.width == 0 || m.height == 0 {
		return
	}
	h, v := appStyle.GetFrameSize()
	m.cpList.SetSize(m.width-h, m.height-v)
}

func (m *Model) sizeFiles() {
	if m.width == 0 || m.height == 0 {
		return
	}
	// Files view does NOT use appStyle wrapping — renders raw to avoid
	// double-padding that pushes the help bar off screen.
	leftW := m.width * 2 / 5
	rightW := m.width - leftW - 1
	m.fileList.SetSize(leftW, m.height)
	m.preview.SetWidth(rightW)
	m.preview.SetHeight(m.height - 2) // title line + margin
}

// ── Commands ────────────────────────────────────────────────────────

func (m *Model) loadFiles(tag string) tea.Cmd {
	return func() tea.Msg {
		info, err := m.source.LoadManifestInfo(tag)
		if err != nil {
			return errMsg{err}
		}

		var items []list.Item
		for li, layer := range info.Layers {
			files, err := m.source.ListLayerFiles(tag, li)
			if err != nil {
				continue
			}
			for _, f := range files {
				items = append(items, fileItem{
					entry:      f,
					layerName:  layer.Name,
					tag:        tag,
					layerIndex: li,
				})
			}
		}

		return filesLoadedMsg{tag, items}
	}
}

func (m Model) previewSelected() tea.Cmd {
	item, ok := m.fileList.SelectedItem().(fileItem)
	if !ok || !item.entry.IsText {
		return nil
	}
	tag := item.tag
	idx := item.layerIndex
	path := item.entry.Path
	wantDiff := m.previewIsDiff

	return func() tea.Msg {
		const max int64 = 64 * 1024
		if wantDiff {
			content, err := m.source.DiffFileContent(tag, idx, path, max)
			if err != nil {
				raw, rawErr := m.source.PreviewFile(tag, idx, path, max)
				if rawErr != nil {
					return errMsg{rawErr}
				}
				return previewMsg{path, string(raw), false}
			}
			return previewMsg{path, content, true}
		}
		raw, err := m.source.PreviewFile(tag, idx, path, max)
		if err != nil {
			return errMsg{err}
		}
		return previewMsg{path, string(raw), false}
	}
}

// ── View ────────────────────────────────────────────────────────────

func (m Model) View() tea.View {
	var content string
	switch m.state {
	case stateCheckpoints:
		content = appStyle.Render(m.cpList.View())
	case stateFiles:
		content = m.viewFiles()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func (m Model) viewFiles() string {
	leftW := m.width * 2 / 5
	rightW := m.width - leftW - 1

	// Update title with filter info
	if m.filterMode {
		m.fileList.Title = fmt.Sprintf("%s / %s_", m.currentTag, m.filterText)
	} else if m.filterText != "" {
		m.fileList.Title = fmt.Sprintf("%s [filter: %s]", m.currentTag, m.filterText)
	} else {
		m.fileList.Title = m.currentTag
	}

	// Left pane: file list (includes its own help bar)
	left := m.fileList.View()

	// Right pane: preview
	var right strings.Builder
	if m.previewTitle != "" {
		mode := "diff"
		if !m.previewIsDiff {
			mode = "raw"
		}
		right.WriteString(dimStyle.Render(fmt.Sprintf(" %s (%s)", m.previewTitle, mode)))
		right.WriteString("\n")
		right.WriteString(m.preview.View())
	} else {
		right.WriteString(dimStyle.Render(" select a text file to preview"))
	}
	if m.errText != "" {
		right.WriteString("\n")
		right.WriteString(errStyle.Render(" " + m.errText))
	}

	// Separator
	sep := sepStyle.Render(strings.Repeat("│\n", m.height))

	// No Height constraint on left — list manages its own height including help bar
	leftPane := lipgloss.NewStyle().Width(leftW).Render(left)
	rightPane := lipgloss.NewStyle().Width(rightW).Height(m.height).Render(right.String())

	// No appStyle wrapping — avoids padding that pushes help off screen
	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, sep, rightPane)
}

// ── Helpers ─────────────────────────────────────────────────────────

func colorizeDiff(content string, isDiff bool) string {
	if !isDiff {
		return content
	}
	var sb strings.Builder
	for _, line := range strings.Split(content, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			sb.WriteString(hdrStyle.Render(line))
		case strings.HasPrefix(line, "@@"):
			sb.WriteString(hdrStyle.Render(line))
		case strings.HasPrefix(line, "+"):
			sb.WriteString(addStyle.Render(line))
		case strings.HasPrefix(line, "-"):
			sb.WriteString(remStyle.Render(line))
		default:
			sb.WriteString(line)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
