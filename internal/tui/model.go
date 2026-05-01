package tui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/zhangjianyong66/mihomo-manager/internal/mihomo"
)

type page int

const (
	mainMenu page = iota
	actionMenu
	resultView
	inputView
)

type Model struct {
	client *mihomo.Client
	page   page
	width  int
	height int

	mainItems   []string
	mainIndex   int
	actionItems []string
	actionIndex int
	actionCtx   string

	input      textinput.Model
	inputTitle string
	inputDo    func(string) (string, error)

	result            string
	err               error
	busy              bool
	returnPage        page
	progressDone      int
	progressTotal     int
	liveResults       []mihomo.NodeDelay
	switchNodeDelay   map[string]int
	switchTestDone    int
	switchTestTotal   int
	switchTestStop    chan struct{}
	currentNodeName   string
	currentGroupName  string
	groups            []mihomo.ProxyGroup
	whitelistItems    []string
	selectedWhitelist string
	logLines          []string
	logRawLines       []string
	logFilter         string
	logRegex          *regexp.Regexp
	logFollowStop     chan struct{}
	logScrollOffset   int
}

type actionDoneMsg struct {
	result string
	err    error
}

type nodeTestMsg struct {
	ch    <-chan mihomo.NodeTestEvent
	event mihomo.NodeTestEvent
	ok    bool
}

type switchNodeTestMsg struct {
	ch    <-chan mihomo.NodeTestEvent
	event mihomo.NodeTestEvent
	ok    bool
}

type logEventMsg struct {
	ch    <-chan mihomo.LogEvent
	event mihomo.LogEvent
	ok    bool
}

func New(client *mihomo.Client) Model {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.CharLimit = 200
	ti.Width = 70
	return Model{client: client, page: mainMenu, returnPage: actionMenu, input: ti, mainItems: []string{"服务管理", "节点管理", "订阅管理", "白名单管理", "路由诊断", "配置管理", "退出"}}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case actionDoneMsg:
		m.busy = false
		m.result = msg.result
		m.err = msg.err
		m.returnPage = actionMenu
		m.page = resultView
		return m, nil
	case nodeTestMsg:
		if !msg.ok {
			m.busy = false
			m.page = resultView
			return m, nil
		}
		if msg.event.Err != nil {
			m.busy = false
			m.err = msg.event.Err
			m.result = "测速失败"
			m.returnPage = actionMenu
			m.page = resultView
			return m, nil
		}
		m.progressDone = msg.event.Done
		m.progressTotal = msg.event.Total
		if msg.event.Result != nil {
			m.liveResults = append(m.liveResults, *msg.event.Result)
		}
		if msg.event.Finished {
			m.busy = false
			if len(m.liveResults) == 0 {
				m.result = "无可用测速结果"
				m.returnPage = actionMenu
				m.page = resultView
				return m, nil
			}
			sortNodeDelays(m.liveResults)
			b := strings.Builder{}
			for i := 0; i < min(10, len(m.liveResults)); i++ {
				b.WriteString(fmt.Sprintf("%d. %s - %dms\n", i+1, m.liveResults[i].Name, m.liveResults[i].Delay))
			}
			m.result = b.String()
			m.returnPage = actionMenu
			m.page = resultView
			return m, nil
		}
		return m, waitNodeTestMsg(msg.ch)
	case switchNodeTestMsg:
		if m.actionCtx != "switch_nodes" && m.actionCtx != "group_nodes" {
			return m, nil
		}
		if !msg.ok {
			return m, nil
		}
		if msg.event.Result != nil {
			if m.switchNodeDelay == nil {
				m.switchNodeDelay = map[string]int{}
			}
			m.switchNodeDelay[msg.event.Result.Name] = msg.event.Result.Delay
		}
		m.switchTestDone = msg.event.Done
		m.switchTestTotal = msg.event.Total
		if msg.event.Finished {
			return m, nil
		}
		return m, waitSwitchNodeTestMsg(msg.ch)
	case logEventMsg:
		if m.actionCtx != "log_live" {
			return m, nil
		}
		if !msg.ok || msg.event.Finished {
			return m, nil
		}
		if msg.event.Err != nil {
			m.result = "日志流读取失败"
			m.err = msg.event.Err
			m.returnPage = actionMenu
			m.page = resultView
			return m, nil
		}
		m.appendLogLine(msg.event.Line)
		return m, waitLogEventMsg(msg.ch)
	case tea.KeyMsg:
		s := msg.String()
		if s == "ctrl+c" {
			return m, tea.Quit
		}
		if m.busy {
			return m, nil
		}
		switch s {
		case "q":
			if m.page == mainMenu {
				return m, tea.Quit
			}
		case "esc":
			if m.actionCtx == "log_live" {
				if m.logFollowStop != nil {
					close(m.logFollowStop)
					m.logFollowStop = nil
				}
				m.actionCtx = ""
				m.actionItems = menuActions("服务管理")
				m.actionIndex = 0
				return m, nil
			}
			if m.page == actionMenu && (m.actionCtx == "switch_nodes" || m.actionCtx == "group_nodes") {
				if m.switchTestStop != nil {
					close(m.switchTestStop)
					m.switchTestStop = nil
				}
				if m.actionCtx == "group_nodes" {
					m.actionCtx = "group_list"
					m.actionItems = formatGroupItems(m.groups)
				} else {
					m.actionCtx = ""
					m.actionItems = menuActions("节点管理")
				}
				m.actionIndex = 0
				m.switchNodeDelay = nil
				m.switchTestDone = 0
				m.switchTestTotal = 0
				return m, nil
			}
			if m.page == actionMenu && m.actionCtx == "group_list" {
				m.actionCtx = ""
				m.actionItems = nil
				m.actionIndex = 0
				m.page = mainMenu
				return m, nil
			}
			if m.page == actionMenu && m.actionCtx == "whitelist_item_menu" {
				m.actionCtx = "whitelist_list"
				m.actionItems = m.whitelistActionItems()
				m.actionIndex = m.findWhitelistIndex(m.selectedWhitelist)
				return m, nil
			}
			if m.page == actionMenu && m.actionCtx == "whitelist_list" {
				m.actionCtx = ""
				m.actionItems = nil
				m.actionIndex = 0
				m.page = mainMenu
				return m, nil
			}
			if m.page == actionMenu || m.page == resultView || m.page == inputView {
				if m.page == inputView {
					if m.actionCtx == "log_filter_input" {
						m.input.Blur()
						m.actionCtx = "log_live"
						m.page = actionMenu
						return m, nil
					}
					if m.actionCtx == "route_diag_input" {
						m.actionCtx = ""
					}
					m.input.Blur()
				}
				m.page = mainMenu
				m.result = ""
				m.err = nil
				return m, nil
			}
		}
		if m.actionCtx == "log_live" {
			switch s {
			case "up":
				m.logScrollOffset++
				return m, nil
			case "down":
				if m.logScrollOffset > 0 {
					m.logScrollOffset--
				}
				return m, nil
			case "/":
				m.inputTitle = "输入日志正则过滤（留空清除）"
				m.input.SetValue(m.logFilter)
				m.input.Focus()
				m.actionCtx = "log_filter_input"
				m.page = inputView
				return m, textinput.Blink
			case "c":
				m.logFilter = ""
				m.logRegex = nil
				m.rebuildLogViewFromRaw()
				return m, nil
			}
		}

		switch m.page {
		case mainMenu:
			return m.updateMain(msg)
		case actionMenu:
			return m.updateAction(msg)
		case inputView:
			return m.updateInput(msg)
		case resultView:
			if s == "enter" {
				m.page = m.returnPage
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	}
	return m, nil
}

func (m Model) updateMain(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	switch s {
	case "up":
		if m.mainIndex > 0 {
			m.mainIndex--
		}
	case "down":
		if m.mainIndex < len(m.mainItems)-1 {
			m.mainIndex++
		}
	case "enter":
		choice := m.mainItems[m.mainIndex]
		if choice == "退出" {
			return m, tea.Quit
		}
		if choice == "节点管理" {
			groups, err := m.client.ListSelectableGroups()
			if err != nil {
				m.result = "读取代理组失败"
				m.err = err
				m.returnPage = mainMenu
				m.page = resultView
				return m, nil
			}
			if len(groups) == 0 {
				m.result = "没有可管理的代理组"
				m.err = nil
				m.returnPage = mainMenu
				m.page = resultView
				return m, nil
			}
			m.groups = groups
			m.actionCtx = "group_list"
			m.actionItems = formatGroupItems(groups)
			m.actionIndex = 0
			m.page = actionMenu
			return m, nil
		}
		if choice == "白名单管理" {
			items, err := m.client.ListWhitelist()
			if err != nil {
				m.result = "读取白名单失败"
				m.err = err
				m.returnPage = mainMenu
				m.page = resultView
				return m, nil
			}
			m.whitelistItems = sortUniqueDomains(items)
			m.actionCtx = "whitelist_list"
			m.actionItems = m.whitelistActionItems()
			m.actionIndex = 0
			m.page = actionMenu
			return m, nil
		}
		if choice == "路由诊断" {
			m.inputTitle = "输入 URL 或域名"
			m.input.SetValue("")
			m.input.Focus()
			m.actionCtx = "route_diag_input"
			m.page = inputView
			return m, textinput.Blink
		}
		m.actionItems = menuActions(choice)
		m.actionIndex = 0
		m.page = actionMenu
	}
	return m, nil
}

func (m Model) updateAction(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	switch s {
	case "a":
		if m.actionCtx == "whitelist_list" {
			m.inputTitle = "新增白名单域名"
			m.inputDo = func(v string) (string, error) {
				if err := m.client.AddWhitelist(v); err != nil {
					return "新增失败", err
				}
				items, err := m.client.ListWhitelist()
				if err == nil {
					m.whitelistItems = sortUniqueDomains(items)
					m.actionItems = m.whitelistActionItems()
				}
				m.actionCtx = "whitelist_list"
				m.actionIndex = m.findWhitelistIndex(normalizeDomainText(v))
				return "已新增", nil
			}
			m.input.SetValue("")
			m.input.Focus()
			m.page = inputView
			return m, textinput.Blink
		}
	case "up":
		if m.actionIndex > 0 {
			m.actionIndex--
		}
	case "down":
		if m.actionIndex < len(m.actionItems)-1 {
			m.actionIndex++
		}
	case "left":
		if (m.actionCtx == "group_list" || m.actionCtx == "group_nodes" || m.actionCtx == "switch_nodes" || m.actionCtx == "whitelist_list") && len(m.actionItems) > 0 {
			m.actionIndex = m.pageMove(-1)
		}
	case "right":
		if (m.actionCtx == "group_list" || m.actionCtx == "group_nodes" || m.actionCtx == "switch_nodes" || m.actionCtx == "whitelist_list") && len(m.actionItems) > 0 {
			m.actionIndex = m.pageMove(1)
		}
	case "enter":
		act := m.actionItems[m.actionIndex]
		if m.actionCtx == "switch_nodes" {
			if act == "返回" {
				if m.switchTestStop != nil {
					close(m.switchTestStop)
					m.switchTestStop = nil
				}
				m.actionCtx = ""
				m.actionItems = menuActions("节点管理")
				m.actionIndex = 0
				m.switchNodeDelay = nil
				m.switchTestDone = 0
				m.switchTestTotal = 0
				return m, nil
			}
			if m.switchTestStop != nil {
				close(m.switchTestStop)
				m.switchTestStop = nil
			}
			m.actionCtx = ""
			m.actionItems = menuActions("节点管理")
			m.actionIndex = 0
			m.switchNodeDelay = nil
			m.switchTestDone = 0
			m.switchTestTotal = 0
			model, cmd := m.executeAction("节点切换选择", act)
			if mm, ok := model.(Model); ok {
				mm.returnPage = mainMenu
				return mm, cmd
			}
			return model, cmd
		}
		if m.actionCtx == "group_list" {
			if act == "返回" {
				m.actionCtx = ""
				m.actionItems = nil
				m.actionIndex = 0
				m.page = mainMenu
				return m, nil
			}
			groupName := parseGroupName(act)
			return m.enterGroupNodes(groupName)
		}
		if m.actionCtx == "group_nodes" {
			if act == "返回" {
				if m.switchTestStop != nil {
					close(m.switchTestStop)
					m.switchTestStop = nil
				}
				m.actionCtx = "group_list"
				m.actionItems = formatGroupItems(m.groups)
				m.actionIndex = 0
				m.switchNodeDelay = nil
				m.switchTestDone = 0
				m.switchTestTotal = 0
				return m, nil
			}
			if m.switchTestStop != nil {
				close(m.switchTestStop)
				m.switchTestStop = nil
			}
			if err := m.client.SwitchNodeInGroup(m.currentGroupName, act); err != nil {
				m.result = "节点切换失败"
				m.err = err
				m.returnPage = actionMenu
				m.page = resultView
				return m, nil
			}
			m.currentNodeName = act
			m.switchNodeDelay = map[string]int{}
			m.switchTestDone = 0
			m.switchTestTotal = 0
			m.switchTestStop = make(chan struct{})
			stream := m.client.TestGroupNodesStreamWithStop(m.currentGroupName, 5, 120, m.switchTestStop)
			return m, waitSwitchNodeTestMsg(stream)
		}
		if m.actionCtx == "whitelist_list" {
			if act == "返回" {
				m.actionCtx = ""
				m.actionItems = nil
				m.actionIndex = 0
				m.page = mainMenu
				return m, nil
			}
			m.selectedWhitelist = act
			m.actionCtx = "whitelist_item_menu"
			m.actionItems = []string{"修改", "删除", "返回"}
			m.actionIndex = 0
			return m, nil
		}
		if m.actionCtx == "whitelist_item_menu" {
			switch act {
			case "返回":
				m.actionCtx = "whitelist_list"
				m.actionItems = m.whitelistActionItems()
				m.actionIndex = m.findWhitelistIndex(m.selectedWhitelist)
				return m, nil
			case "删除":
				if err := m.client.RemoveWhitelist(m.selectedWhitelist); err != nil {
					m.result = "删除失败"
					m.err = err
				} else {
					m.result = "已删除"
					m.err = nil
				}
				items, _ := m.client.ListWhitelist()
				m.whitelistItems = sortUniqueDomains(items)
				m.actionCtx = "whitelist_list"
				m.actionItems = m.whitelistActionItems()
				m.actionIndex = 0
				m.returnPage = actionMenu
				m.page = resultView
				return m, nil
			case "修改":
				old := m.selectedWhitelist
				m.inputTitle = "修改白名单域名"
				m.inputDo = func(v string) (string, error) {
					newDomain := normalizeDomainText(v)
					if newDomain == "" {
						return "修改失败", fmt.Errorf("域名不能为空")
					}
					if err := m.client.RemoveWhitelist(old); err != nil {
						return "修改失败", err
					}
					if err := m.client.AddWhitelist(newDomain); err != nil {
						return "修改失败", err
					}
					items, err := m.client.ListWhitelist()
					if err == nil {
						m.whitelistItems = sortUniqueDomains(items)
						m.actionItems = m.whitelistActionItems()
					}
					m.selectedWhitelist = newDomain
					m.actionCtx = "whitelist_list"
					m.actionIndex = m.findWhitelistIndex(newDomain)
					return "已修改", nil
				}
				m.input.SetValue(old)
				m.input.Focus()
				m.page = inputView
				return m, textinput.Blink
			}
		}
		if act == "返回" {
			m.page = mainMenu
			return m, nil
		}
		return m.executeAction(m.mainItems[m.mainIndex], act)
	}
	return m, nil
}

func (m Model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if msg.String() == "enter" && m.actionCtx == "log_filter_input" {
		pattern := strings.TrimSpace(m.input.Value())
		if pattern == "" {
			m.logFilter = ""
			m.logRegex = nil
			m.rebuildLogViewFromRaw()
			m.input.SetValue("")
			m.input.Blur()
			m.actionCtx = "log_live"
			m.page = actionMenu
			return m, nil
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			m.result = "正则无效"
			m.err = err
			m.returnPage = actionMenu
			m.page = resultView
			m.actionCtx = "log_live"
			m.input.Blur()
			return m, nil
		}
		m.logFilter = pattern
		m.logRegex = re
		m.rebuildLogViewFromRaw()
		m.input.SetValue("")
		m.input.Blur()
		m.actionCtx = "log_live"
		m.page = actionMenu
		return m, nil
	}
	if msg.String() == "enter" && m.actionCtx == "route_diag_input" {
		val := strings.TrimSpace(m.input.Value())
		m.input.SetValue("")
		m.input.Blur()
		m.actionCtx = ""
		if val == "" {
			m.result = "诊断失败"
			m.err = fmt.Errorf("请输入 URL 或域名")
			m.returnPage = mainMenu
			m.page = resultView
			return m, nil
		}
		res, err := m.client.DiagnoseRoute(val)
		if err != nil {
			m.result = "诊断失败"
			m.err = err
			m.returnPage = mainMenu
			m.page = resultView
			return m, nil
		}
		out := strings.Builder{}
		out.WriteString(fmt.Sprintf("输入: %s\n", res.Input))
		out.WriteString(fmt.Sprintf("Host: %s\n", res.Host))
		if res.MatchedRule != "" {
			out.WriteString(fmt.Sprintf("命中规则: %s\n", res.MatchedRule))
		}
		if res.Target != "" {
			out.WriteString(fmt.Sprintf("目标组: %s\n", res.Target))
		}
		if res.CurrentNode != "" {
			out.WriteString(fmt.Sprintf("当前节点: %s\n", res.CurrentNode))
		}
		if res.Confidence != "" {
			out.WriteString(fmt.Sprintf("置信度: %s\n", res.Confidence))
		}
		if strings.TrimSpace(res.Note) != "" {
			out.WriteString(fmt.Sprintf("备注: %s\n", strings.TrimSpace(res.Note)))
		}
		m.result = out.String()
		m.err = nil
		m.returnPage = mainMenu
		m.page = resultView
		return m, nil
	}
	if msg.String() == "enter" && m.inputDo != nil {
		res, err := m.inputDo(m.input.Value())
		m.result = res
		m.err = err
		m.returnPage = actionMenu
		m.page = resultView
		m.input.SetValue("")
		m.input.Blur()
	}
	return m, cmd
}

func (m Model) executeAction(cat, act string) (tea.Model, tea.Cmd) {
	setResult := func(s string, err error) (tea.Model, tea.Cmd) {
		m.result = s
		m.err = err
		m.returnPage = actionMenu
		m.page = resultView
		return m, nil
	}

	switch cat {
	case "服务管理":
		switch act {
		case "状态":
			return setResult("服务状态: "+m.client.ServiceStatus(), nil)
		case "启动":
			return setResult("已启动", m.client.Start())
		case "停止":
			return setResult("已停止", m.client.Stop())
		case "重启":
			return setResult("已重启", m.client.Restart())
		case "热重载":
			return setResult("已热重载", m.client.Reload())
		case "配置测试":
			return setResult("配置测试通过", m.client.TestConfig())
		case "查看实时日志":
			m.logLines = nil
			m.logRawLines = nil
			m.logFilter = ""
			m.logRegex = nil
			m.logScrollOffset = 0
			if m.logFollowStop != nil {
				close(m.logFollowStop)
			}
			m.logFollowStop = make(chan struct{})
			m.actionCtx = "log_live"
			m.page = actionMenu
			stream := m.client.TailLogsStreamWithStop(50, m.logFollowStop)
			return m, waitLogEventMsg(stream)
		}
	case "节点管理":
		return m, nil
	case "节点切换选择":
		return setResult("节点已切换: "+act, m.client.SwitchNode(act))
	case "订阅管理":
		switch act {
		case "保存订阅URL":
			m.inputTitle = "输入订阅 URL"
			m.inputDo = func(v string) (string, error) { return "订阅 URL 已保存", m.client.SaveSubscriptionURL(v) }
			m.input.SetValue("")
			m.input.Focus()
			m.page = inputView
			return m, textinput.Blink
		case "查看订阅URL":
			v, err := m.client.ReadSubscriptionURL()
			return setResult("当前 URL: "+v, err)
		case "更新订阅":
			if err := m.client.UpdateSubscription(); err != nil {
				return setResult("订阅更新失败", err)
			}
			if err := m.client.Reload(); err != nil {
				return setResult("订阅更新成功，但热重载失败", err)
			}
			return setResult("订阅更新并热重载完成", nil)
		}
	case "白名单管理":
		return m, nil
	case "配置管理":
		switch act {
		case "备份配置":
			return setResult("配置已备份", m.client.BackupConfig())
		case "恢复配置":
			return setResult("配置已恢复", m.client.RestoreConfig())
		case "编辑配置":
			return setResult("编辑结束", m.client.OpenConfigEditor())
		case "应用分流规则（大陆直连/其他走GLOBAL）":
			return setResult("分流规则已应用", m.client.ApplyRouteCN())
		}
	}
	return setResult("未实现操作", nil)
}

func menuActions(main string) []string {
	switch main {
	case "服务管理":
		return []string{"状态", "启动", "停止", "重启", "热重载", "配置测试", "查看实时日志", "返回"}
	case "节点管理":
		return []string{"返回"}
	case "订阅管理":
		return []string{"保存订阅URL", "查看订阅URL", "更新订阅", "返回"}
	case "白名单管理":
		return []string{"返回"}
	case "配置管理":
		return []string{"备份配置", "恢复配置", "编辑配置", "应用分流规则（大陆直连/其他走GLOBAL）", "返回"}
	default:
		return []string{"返回"}
	}
}

func (m Model) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")).Render("Mihomo Manager (Interactive)")
	if m.page == inputView {
		return fmt.Sprintf("%s\n\n%s\n%s\n\nEsc 返回", title, m.inputTitle, m.input.View())
	}
	if m.page == resultView {
		msg := m.result
		if m.err != nil {
			msg = "错误: " + m.err.Error() + "\n" + m.result
		}
		if m.busy {
			sortNodeDelays(m.liveResults)
			b := strings.Builder{}
			for i := 0; i < min(10, len(m.liveResults)); i++ {
				b.WriteString(fmt.Sprintf("%d. %s - %dms\n", i+1, m.liveResults[i].Name, m.liveResults[i].Delay))
			}
			progress := fmt.Sprintf("进度: %d/%d", m.progressDone, m.progressTotal)
			if m.progressTotal == 0 {
				progress = "进度: 初始化中..."
			}
			return fmt.Sprintf("%s\n\n%s\n%s\n\n%s", title, progress, b.String(), "测速进行中，结果实时刷新...")
		}
		return fmt.Sprintf("%s\n\n%s\n\nEnter 返回主菜单", title, msg)
	}

	items := m.mainItems
	idx := m.mainIndex
	header := "主菜单"
	start := 0
	end := len(items)
	if m.page == actionMenu {
		items = m.actionItems
		idx = m.actionIndex
		header = "操作菜单"
		if m.actionCtx == "group_list" {
			header = "节点管理 / 分组列表"
		}
		if m.actionCtx == "group_nodes" {
			header = fmt.Sprintf("代理组: %s（当前: %s）", m.currentGroupName, strings.TrimSpace(m.currentNodeName))
		}
		if m.actionCtx == "whitelist_list" {
			header = "白名单管理 / 列表"
		}
		if m.actionCtx == "whitelist_item_menu" {
			header = fmt.Sprintf("白名单操作: %s", m.selectedWhitelist)
		}
		if m.actionCtx == "log_live" {
			header = "服务管理 / 实时日志"
		}
		pageSize := m.pageSize()
		start, end = pageWindow(idx, len(items), pageSize)
	}
	if m.page == actionMenu && m.actionCtx == "log_live" {
		return m.renderLogView(title)
	}
	var b strings.Builder
	for i := start; i < end; i++ {
		it := items[i]
		if m.page == actionMenu && m.actionCtx == "group_nodes" {
			it = m.renderSwitchNodeItem(it)
		}
		cursor := "  "
		if i == idx {
			cursor = "> "
		}
		b.WriteString(cursor + it + "\n")
	}
	footer := "↑/↓ 选择  Enter 进入  Esc 返回  q 退出"
	if m.page == actionMenu {
		pageSize := m.pageSize()
		totalPages := (len(items) + pageSize - 1) / pageSize
		currentPage := (idx / pageSize) + 1
		footer = fmt.Sprintf("↑/↓ 选择 Enter 进入 Esc 返回 q 退出 | 第 %d/%d 页", currentPage, max(1, totalPages))
		if m.actionCtx == "group_nodes" {
			footer = fmt.Sprintf("%s | ←/→ 翻页 | 进度 %d/%d", footer, m.switchTestDone, m.switchTestTotal)
		} else if m.actionCtx == "group_list" {
			footer = fmt.Sprintf("%s | ←/→ 翻页", footer)
		} else if m.actionCtx == "whitelist_list" {
			footer = fmt.Sprintf("%s | ←/→ 翻页 | a 新增", footer)
		}
	}
	footer = fitFooter(footer, m.width)
	return fmt.Sprintf("%s\n\n%s\n%s\n\n%s", title, header, b.String(), footer)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func pageWindow(index, total, pageSize int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	start := (index / pageSize) * pageSize
	end := min(start+pageSize, total)
	return start, end
}

func (m Model) pageSize() int {
	// 预留标题和提示行，避免超屏
	if m.height <= 0 {
		return 12
	}
	n := m.height - 8
	if n < 5 {
		n = 5
	}
	return n
}

func (m Model) pageMove(dir int) int {
	total := len(m.actionItems)
	if total == 0 {
		return 0
	}
	ps := m.pageSize()
	if ps <= 0 {
		ps = 10
	}
	offset := m.actionIndex % ps
	pageStart := (m.actionIndex / ps) * ps
	targetStart := pageStart + dir*ps
	if targetStart < 0 {
		targetStart = pageStart
	}
	if targetStart >= total {
		targetStart = pageStart
	}
	target := targetStart + offset
	if target >= total {
		target = total - 1
	}
	if target < 0 {
		target = 0
	}
	return target
}

func waitNodeTestMsg(ch <-chan mihomo.NodeTestEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		return nodeTestMsg{ch: ch, event: ev, ok: ok}
	}
}

func waitSwitchNodeTestMsg(ch <-chan mihomo.NodeTestEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		return switchNodeTestMsg{ch: ch, event: ev, ok: ok}
	}
}

func waitLogEventMsg(ch <-chan mihomo.LogEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		return logEventMsg{ch: ch, event: ev, ok: ok}
	}
}

func sortNodeDelays(in []mihomo.NodeDelay) {
	for i := 0; i < len(in); i++ {
		for j := i + 1; j < len(in); j++ {
			if in[j].Delay < in[i].Delay {
				in[i], in[j] = in[j], in[i]
			}
		}
	}
}

func (m Model) renderSwitchNodeItem(node string) string {
	if node == "返回" {
		return node
	}
	suffix := ""
	if strings.TrimSpace(node) == strings.TrimSpace(m.currentNodeName) {
		suffix = " ✅"
	}
	if d, ok := m.switchNodeDelay[node]; ok {
		if d > 0 {
			return fmt.Sprintf("%s%s  [%dms]", node, suffix, d)
		}
		return fmt.Sprintf("%s%s  [timeout]", node, suffix)
	}
	return fmt.Sprintf("%s%s  [...]", node, suffix)
}

func fitFooter(s string, width int) string {
	if width <= 0 {
		return s
	}
	maxw := width - 2
	if len([]rune(s)) <= maxw {
		return s
	}
	compact := strings.NewReplacer(
		"↑/↓ 选择 Enter 进入 Esc 返回 q 退出", "↑/↓ Enter Esc q",
		"第 ", "P",
		" 页", "",
		" | ←/→ 翻页 | ", " | ←/→ | ",
		"进度 ", "",
	).Replace(s)
	if len([]rune(compact)) <= maxw {
		return compact
	}
	r := []rune(compact)
	if maxw <= 3 {
		return string(r[:max(0, maxw)])
	}
	return string(r[:maxw-3]) + "..."
}

func formatGroupItems(groups []mihomo.ProxyGroup) []string {
	out := make([]string, 0, len(groups)+1)
	for _, g := range groups {
		now := strings.TrimSpace(g.Now)
		if now == "" {
			now = "-"
		}
		out = append(out, fmt.Sprintf("%s (%s) -> %s", g.Name, g.Type, now))
	}
	out = append(out, "返回")
	return out
}

func parseGroupName(item string) string {
	i := strings.Index(item, " (")
	if i <= 0 {
		return strings.TrimSpace(item)
	}
	return strings.TrimSpace(item[:i])
}

func (m Model) enterGroupNodes(groupName string) (tea.Model, tea.Cmd) {
	nodes, now, err := m.client.GroupNodes(groupName)
	if err != nil {
		m.result = "读取代理组节点失败"
		m.err = err
		m.returnPage = actionMenu
		m.page = resultView
		return m, nil
	}
	if len(nodes) == 0 {
		m.result = "该代理组没有可选节点"
		m.err = nil
		m.returnPage = actionMenu
		m.page = resultView
		return m, nil
	}
	m.currentGroupName = groupName
	m.currentNodeName = strings.TrimSpace(now)
	m.actionCtx = "group_nodes"
	m.actionItems = append(nodes, "返回")
	m.actionIndex = 0
	m.switchNodeDelay = map[string]int{}
	m.switchTestDone = 0
	m.switchTestTotal = 0
	m.switchTestStop = make(chan struct{})
	stream := m.client.TestGroupNodesStreamWithStop(groupName, 5, 120, m.switchTestStop)
	return m, waitSwitchNodeTestMsg(stream)
}

func (m Model) whitelistActionItems() []string {
	out := make([]string, 0, len(m.whitelistItems)+1)
	out = append(out, m.whitelistItems...)
	out = append(out, "返回")
	return out
}

func (m Model) findWhitelistIndex(domain string) int {
	for i, d := range m.whitelistItems {
		if strings.EqualFold(strings.TrimSpace(d), strings.TrimSpace(domain)) {
			return i
		}
	}
	return 0
}

func sortUniqueDomains(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, d := range items {
		n := normalizeDomainText(d)
		if n == "" || seen[strings.ToLower(n)] {
			continue
		}
		seen[strings.ToLower(n)] = true
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

func normalizeDomainText(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "https://")
	v = strings.TrimPrefix(v, "http://")
	if i := strings.Index(v, "/"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

func (m *Model) appendLogLine(line string) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return
	}
	m.logRawLines = append(m.logRawLines, line)
	const maxLogBuffer = 500
	if len(m.logRawLines) > maxLogBuffer {
		m.logRawLines = m.logRawLines[len(m.logRawLines)-maxLogBuffer:]
	}
	if m.logRegex != nil && !m.logRegex.MatchString(line) {
		return
	}
	m.logLines = append(m.logLines, line)
	if len(m.logLines) > maxLogBuffer {
		m.logLines = m.logLines[len(m.logLines)-maxLogBuffer:]
	}
}

func (m *Model) rebuildLogViewFromRaw() {
	m.logLines = make([]string, 0, len(m.logRawLines))
	for _, line := range m.logRawLines {
		if m.logRegex != nil && !m.logRegex.MatchString(line) {
			continue
		}
		m.logLines = append(m.logLines, line)
	}
	const maxLogBuffer = 500
	if len(m.logLines) > maxLogBuffer {
		m.logLines = m.logLines[len(m.logLines)-maxLogBuffer:]
	}
}

func (m Model) renderLogView(title string) string {
	lines := m.logLines
	h := m.height - 8
	if h < 5 {
		h = 5
	}
	total := len(lines)
	if m.logScrollOffset < 0 {
		m.logScrollOffset = 0
	}
	maxOffset := 0
	if total > h {
		maxOffset = total - h
	}
	if m.logScrollOffset > maxOffset {
		m.logScrollOffset = maxOffset
	}
	start := max(0, total-h-m.logScrollOffset)
	end := min(total, start+h)
	view := ""
	if start >= end {
		view = "(暂无日志)"
	} else {
		view = strings.Join(lines[start:end], "\n")
	}
	filter := m.logFilter
	if filter == "" {
		filter = "(none)"
	}
	footer := fmt.Sprintf("↑/↓ 滚动  / 设置正则  c 清除过滤  Esc 返回  Ctrl+C 退出 | lines %d | filter %s", total, filter)
	footer = fitFooter(footer, m.width)
	return fmt.Sprintf("%s\n\n服务管理 / 实时日志\n%s\n\n%s", title, view, footer)
}
