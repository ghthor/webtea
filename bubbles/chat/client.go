package chat

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"charm.land/log/v2"
	"github.com/ghthor/webtea/bubbles/blokfall"
	"github.com/ghthor/webtea/mpty"
	"github.com/ghthor/webtea/mpty/mptymsg"
	"github.com/ghthor/webtea/unsafering"
)

type ChatSizeMsg struct {
	Width, Height int
}

var (
	Bold       = lipgloss.NewStyle().Bold(true)
	None       = lipgloss.NewStyle()
	AlignRight = lipgloss.NewStyle().Align(lipgloss.Right)

	StyleZeroWidth = lipgloss.NewStyle().Padding(0).Margin(0).Width(0).MaxWidth(0)

	StyleDebugCol = lipgloss.NewStyle().
			MarginRight(1)

	StyleTSCol = lipgloss.NewStyle().
			Faint(true).
			MarginRight(1).
			Align(lipgloss.Right).
			Width(len(time.TimeOnly) + 1).
			MaxWidth(len(time.TimeOnly) + 1)

	VertLine = lipgloss.Border{
		Left:        "│",
		TopRight:    "│",
		Right:       "│",
		BottomRight: "│",
		MiddleRight: "│",
	}

	StyleNick = lipgloss.NewStyle().
			Align(lipgloss.Right).
			Margin(0).
			MarginRight(1).
			Padding(0).
			PaddingRight(1).
			PaddingLeft(0).
			Border(VertLine, false).
			BorderRight(true)

	StyleSysNick = StyleNick.Faint(true)

	StyleMsgCol = lipgloss.NewStyle().
			Align(lipgloss.Left).
			Margin(0).
			Padding(0).
			PaddingLeft(0).
			PaddingRight(1)
	StyleSysMsg = StyleMsgCol.Faint(true)
)

func NewClient(ctx context.Context, info *mpty.ClientInfoModel, cmds ...Cmd) *Client {
	m := &Client{
		ctx: ctx,

		info: info,

		table:    table.New(),
		chatData: newChatData(300),
	}
	m.SetupCmdPalette(cmds...)
	return m
}

type chatData struct {
	*unsafering.Buffer[Msg]
	nickWidths *unsafering.Buffer[int]
	nickWidth  int
}

func newChatData(sz int) *chatData {
	return &chatData{
		Buffer:     unsafering.New[Msg](sz),
		nickWidths: unsafering.New[int](sz),
	}
}

func (c *chatData) Push(m Msg) {
	c.Buffer.Push(m)
	c.nickWidths.Push(lipgloss.Width(m.Nick()))
	c.nickWidth = c.NickMaxWidth()
}

func (c chatData) NickMaxWidth() int {
	w := 0
	for m := range c.nickWidths.Iter() {
		w = max(w, m)
	}
	return w
}

type Client struct {
	info *mpty.ClientInfoModel

	b strings.Builder

	Width, Height int

	Send mpty.Input

	ctx context.Context

	cmds []tea.Cmd

	cmdLine    textinput.Model
	cmdPalette CmdPalette

	table *table.Table
	view  viewport.Model

	chatData *chatData

	blokfallView      blokfall.MPView
	blokfallConnected bool

	quiet         bool
	showTimestamp bool

	debug bool

	err error
}

var _ table.Data = &Client{}
var _ mpty.ClientModel = &Client{}

func (m *Client) Id() mpty.ClientId {
	return m.info.Id()
}

func (m *Client) Err() error {
	return m.err
}

func (m *Client) AtRaw(row int) Msg {
	msg, _ := m.chatData.AtInWindow(row, m.chatData.Len())
	return msg
}

const (
	COL_DEBUG = iota
	COL_TS
	COL_WHO
	COL_MSG
	COL_SZ
)

func (m *Client) At(row, cell int) string {
	msg := m.AtRaw(row)

	switch cell {
	case COL_DEBUG:
		if m.debug {
			return fmt.Sprintf("%d", msg.recId)
		}
	case COL_TS:
		if !m.showTimestamp {
			return ""
		}
		if msg.At.IsZero() {
			return ""
		}
		return msg.At.Format(time.TimeOnly)
	case COL_WHO:
		return msg.Nick()
	case COL_MSG:
		return msg.Str
	default:
	}

	return ""
}

func (m *Client) Rows() int {
	return m.chatData.Len()
}

func (m *Client) Columns() int {
	return COL_SZ
}

func (m *Client) styleFunc(row, col int) lipgloss.Style {
	msg := m.AtRaw(row)

	switch col {
	case COL_DEBUG:
		if m.debug {
			return StyleDebugCol
		} else {
			return StyleZeroWidth
		}
	case COL_TS:
		if m.showTimestamp {
			return StyleTSCol
		} else {
			return StyleZeroWidth
		}
	case COL_WHO:
		s := StyleNick
		switch msg.Who {
		case SysNick, InfoNick, HelpNick:
			s = StyleSysNick
		}
		// return s
		width := m.chatData.nickWidth + 1 + 1 + 1 // padding + border
		return s.
			Width(width)

	case COL_MSG:
		switch msg.Who {
		case SysNick, InfoNick, HelpNick:
			return StyleSysMsg
		}
		return StyleMsgCol

	default:
	}

	return None
}

func (m *Client) setTableOffset() {
	m.table.YOffset(max(0, m.chatData.Len()-m.ChatViewHeight()-1))
}

func (m *Client) Init() tea.Cmd {
	if m.cmds == nil {
		m.cmds = make([]tea.Cmd, 0, 1)
	}

	// TODO: dynamic suggestions
	m.cmdLine = textinput.New()
	m.cmdLine.Prompt = "> "
	m.cmdLine.Placeholder = "/help"
	m.cmdLine.CharLimit = 0
	m.cmdLine.ShowSuggestions = true

	// TODO: there is bug where it wraps early, I think it something to do with the empty border?
	m.table = m.table.
		Border(lipgloss.Border{}).
		Headers().
		BorderHeader(false).
		BorderTop(false).
		BorderLeft(false).
		BorderRow(false).
		BorderColumn(false).
		BorderRight(false).
		BorderBottom(true). // Blankline
		Wrap(true).
		Data(m).
		StyleFunc(m.styleFunc)

	m.view = viewport.New(
		viewport.WithWidth(m.Width),
		viewport.WithHeight(m.ChatViewHeight()),
	)

	return tea.Batch(m.cmdLine.Focus())
}

func (m *Client) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.UpdateClient(msg)
}

func (m *Client) UpdateChat(msg tea.Msg) (*Client, tea.Cmd) {
	_, cmd := m.UpdateClient(msg)
	return m, cmd
}

func (m *Client) UpdateClient(msg tea.Msg) (mpty.ClientModel, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds = m.cmds[:0]
	)

	m.info, cmd = m.info.UpdateInfo(msg)
	cmds = append(cmds, cmd)

	switch msg := msg.(type) {
	case mpty.Input:
		m.Send = msg

	case ChatSizeMsg:
		m.SetSize(msg.Width, msg.Height)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			cmds = append(cmds, m.cmdLineExecute())
			if m.blokfallConnected && m.cmdLine.Focused() {
				m.cmdLine.Blur()
			}
		case m.cmdPalette.leader:
			if m.blokfallConnected && !m.cmdLine.Focused() {
				cmds = append(cmds, m.cmdLine.Focus())
			}
		}

	case blokfall.MPView:
		m.blokfallView = msg

	case []mptymsg.Recordable:
		// Initial Messages from recorded datastorage
		for _, msg := range msg {
			switch msg := msg.(type) {
			case Msg:
				m.chatData.Push(msg)
			}
		}

	case []tea.Msg:
		for _, msg := range msg {
			switch msg := msg.(type) {
			case time.Time:
				m.info, cmd = m.info.UpdateInfo(msg)
				cmds = append(cmds, cmd)
			case Msg:
				switch msg.Who {
				case SysNick:
					if m.quiet {
						break
					}
					fallthrough
				default:
					m.chatData.Push(msg)
				}
			case NamesReq:
				if msg.Requestor == m.Id() {
					m.chatData.Push(SysMsg(m.info.Time,
						fmt.Sprintf("-> %d connected: %s", len(msg.Names), strings.Join(msg.Names, ", ")),
					))
				}
			case WhoisReq:
				if msg.Requestor == m.Id() {
					if len(msg.Results) == 0 {
						m.PrintInfoMsg("user not found")
					} else {
						m.PrintInfoMsg("\n" + strings.Join(msg.Results, "\n"))
					}
				}
			case blokfall.MPView:
				m.blokfallView = msg

			case mpty.ClientConnectMsg:
			case mpty.ClientDisconnectMsg:

			case error:
				m.err = msg
				log.Warn("client fatal", "error", msg, "who", m.info.Who.UserProfile.LoginName, "sess", m.info.Sess.RemoteAddr().String())
				return m, tea.Quit
			default:
				log.Warnf("unhandled broadcast message type: %T", msg)
			}
		}
		m.setTableOffset()
	}

	m.cmdLine, cmd = m.cmdLine.Update(msg)
	cmds = append(cmds, cmd)
	m.updateSuggestions(msg)

	cmds = append(cmds, m.updateBlokFall(msg))

	m.cmds = cmds
	return m, tea.Batch(cmds...)
}

func (m *Client) updateBlokFall(msg tea.Msg) tea.Cmd {
	if !m.blokfallConnected {
		return nil
	}

	if key, ok := msg.(tea.KeyMsg); ok && !m.cmdLine.Focused() {
		return sendMsgCmd(m.ctx, m.Send, blokfall.MPInput{
			Id: m.Id(),
			// TODO: enable key remapping??
			Cmd: blokfall.Input(key.String()),
		})
	}

	return nil

}

func (m *Client) View() tea.View {
	b := &m.b
	b.Reset()

	m.ViewTo(b)
	return tea.View{
		Content: b.String(),
	}
}

func (m *Client) ViewTo(w io.Writer) {
	// TODO: guard with render bool
	t := m.table.Render()
	t = lipgloss.PlaceVertical(m.ChatViewHeight(), lipgloss.Bottom, t)
	m.view.SetContent(t)
	m.view.GotoBottom()
	v := m.view.View()

	if m.blokfallView != nil {
		chatView := lipgloss.Place(
			m.Width, m.ChatViewHeight(),
			lipgloss.Left, lipgloss.Bottom,
			v,
		)

		blokView := *m.blokfallView
		const xOffset = 10
		// horz => right with 10 margin on the right
		x := max(0, m.Width-lipgloss.Width(blokView)-xOffset)
		// vert => centered
		y := max(0, (m.ChatViewHeight()-lipgloss.Height(blokView))/2)

		// TODO: the compositor is destructive instead of what I was hoping
		// which was additive. Meaning the blockfall view occludes the chat
		comp := lipgloss.NewCompositor(
			lipgloss.NewLayer(chatView).X(0).Y(0).Z(0),
			lipgloss.NewLayer(blokView).X(x).Y(y).Z(1),
		)
		comp.Render()

		fmt.Fprintln(w, comp.Render())
	} else {
		fmt.Fprintln(w, v)
	}

	fmt.Fprint(w, m.cmdLine.View())
}

func (m *Client) ChatViewHeight() int {
	// win H - cmdline H
	return max(0, m.Height-1)
}

func (m *Client) SetSize(w, h int) {
	m.Width = w
	m.Height = h
	m.table.Width(w)
	m.cmdLine.SetWidth(w)

	m.viewportResize()
	m.setTableOffset()
}

func (m *Client) viewportResize() {
	m.view.SetHeight(m.ChatViewHeight())
	m.view.SetWidth(m.Width)
}

func (m *Client) updateSuggestions(msg tea.Msg) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case m.cmdPalette.leader:
			if m.cmdLine.Value() == m.cmdPalette.leader {
				m.cmdLine.SetSuggestions(m.cmdPalette.Suggestions())
			}
		}
	}
}

func (m *Client) cmdLineExecute() tea.Cmd {
	if !m.cmdLine.Focused() {
		return nil
	}

	defer func() {
		m.cmdLine.Reset()
		m.cmdLine.SetSuggestions(nil)
	}()

	value := m.cmdLine.Value()

	if !strings.HasPrefix(value, m.cmdPalette.leader) {
		return m.sendChatCmd(value)
	}

	argsStr := strings.TrimPrefix(value, m.cmdPalette.leader)

	cmd, _, _ := strings.Cut(argsStr, " ")
	if cmd == "" {
		return nil
	}

	// TODO: maybe style these type of messages differently?
	m.chatData.Push(Msg{
		At:   m.info.Time,
		Who:  m.info.Who.UserProfile.LoginName,
		Sess: m.info.Sess.RemoteAddr().String(),
		Str:  value,
	})

	c := m.cmdPalette.Find(cmd)
	if c != nil {
		return c.Run(c, strings.Split(argsStr, " "))
	}

	return nil
}

func (m *Client) PrintInfoMsg(s string) {
	m.chatData.Push(InfoMsg(m.info.Time, s))
}

func (m *Client) PrintErrMsg(err error) {
	m.chatData.Push(ErrMsg(m.info.Time, err))
}

func (m *Client) sendChatCmd(msg string) tea.Cmd {
	var (
		who  = m.info.Who.UserProfile.LoginName
		sess = m.info.Sess.RemoteAddr().String()
		now  = time.Now()
		chat = Msg{
			At:   now,
			Who:  who,
			Sess: sess,
			Str:  msg,
		}.SetNick()

		send = m.Send
	)
	if send == nil {
		// TODO: maybe buffer locally till we get a send channel?
		return nil
	}

	return func() tea.Msg {
		sendMsg(m.ctx, send, chat)
		return nil
	}
}

func (m *Client) sendCountCmd(i int) tea.Cmd {
	var (
		who  = m.info.Who.UserProfile.LoginName
		sess = m.info.Sess.RemoteAddr().String()

		send = m.Send
	)
	if send == nil {
		// TODO: maybe buffer locally till we get a send channel?
		return nil
	}

	return func() tea.Msg {
		for v := range i {
			chat := Msg{
				At:   time.Now(),
				Who:  who,
				Sess: sess,
				Str:  fmt.Sprint(v),
			}.SetNick()
			sendMsg(m.ctx, send, chat)
		}
		return nil
	}
}

func sendMsg(ctx context.Context, send mpty.Input, msg tea.Msg) {
	select {
	case <-ctx.Done():
	case send <- msg:
	}
}

func sendMsgCmd(ctx context.Context, send mpty.Input, msg tea.Msg) tea.Cmd {
	return func() tea.Msg {
		sendMsg(ctx, send, msg)
		return nil
	}
}
