package tui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/zhangjianyong66/mihomo-manager/internal/app"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

type Capabilities interface {
	RestartDaemon(context.Context, app.DaemonRestartProgressFunc) (app.DaemonRestartResult, error)
	ModeStatus(context.Context, string) (app.RoutingModeStatus, error)
	SetMode(context.Context, app.SetRoutingModeRequest) (app.RoutingModeStatus, error)
	CoreStatus(context.Context, string) (app.CoreStatus, error)
	CoreAction(context.Context, string, string) error
	ValidateConfig(context.Context, string) error
	Groups(context.Context, string) ([]app.Group, error)
	Group(context.Context, string, string) (app.Group, error)
	SelectGroupNode(context.Context, string, string, string) error
	TestNodes(context.Context, app.NodeTestRequest) <-chan app.NodeTestEvent
	Subscription(context.Context, string) (app.Subscription, error)
	SetSubscription(context.Context, string, string) error
	UpdateSubscription(context.Context, string) error
	Whitelist(context.Context, string) ([]string, error)
	AddWhitelist(context.Context, string, string) error
	RemoveWhitelist(context.Context, string, string) error
	EditWhitelist(context.Context, string, string, string) error
	ApplyRoutePreset(context.Context, string, string) error
	DiagnoseRoute(context.Context, string, string) (app.RouteDiagnosis, error)
	FollowConnections(context.Context, app.ConnectionRequest) <-chan app.ConnectionEvent
	ConfigBackup(context.Context, string) error
	ConfigRestore(context.Context, string) error
	ListenerPorts(context.Context, string) (app.ListenerPortStatus, error)
	SetListenerPort(context.Context, app.SetListenerPortRequest) (app.ListenerPortStatus, error)
	ProxyStatus(context.Context, string, string) (app.ProxyConfigStatus, error)
	SetProxy(context.Context, app.ProxyRequest) (app.ProxyConfigStatus, error)
	RestoreProxy(context.Context, app.ProxyRequest) (app.ProxyConfigStatus, error)
	DisableProxy(context.Context, app.ProxyRequest) (app.ProxyConfigStatus, error)
	FollowLogs(context.Context, app.LogRequest) <-chan app.LogEvent
	EditConfig(context.Context, string) error
}

type page int

const (
	mainMenu page = iota
	actionMenu
	resultView
	inputView
)

type nodeTestMode uint8

const (
	nodeTestIdle nodeTestMode = iota
	nodeTestSingle
	nodeTestBatch
)

var nodeCursorStyle = lipgloss.NewStyle().
	Background(lipgloss.Color("#0B7285")).
	Foreground(lipgloss.Color("#FFFFFF"))

type Model struct {
	client Capabilities
	ctx    context.Context
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
	inputDo    func(string) tea.Cmd

	result                 string
	err                    error
	busy                   bool
	returnPage             page
	progressDone           int
	progressTotal          int
	liveResults            []app.NodeDelay
	nodeResults            map[string]app.NodeDelay
	nodeTestable           map[string]bool
	nodeTestMode           nodeTestMode
	nodeTestCurrent        domain.NodeID
	nodeTestQueue          []domain.NodeID
	switchTestDone         int
	switchTestTotal        int
	switchTestCancel       context.CancelFunc
	switchTestGeneration   uint64
	switchTestMessage      string
	now                    func() time.Time
	currentNodeName        string
	currentGroupID         string
	currentGroupName       string
	groups                 []app.Group
	whitelistItems         []string
	selectedWhitelist      string
	logLines               []string
	logRawLines            []string
	logFilter              string
	logRegex               *regexp.Regexp
	logFollowCancel        context.CancelFunc
	logScrollOffset        int
	modeStatus             app.RoutingModeStatus
	modeSelected           domain.RoutingMode
	modeClose              bool
	modeResult             string
	modeErr                error
	modeCancel             context.CancelFunc
	connectionFollowCancel context.CancelFunc
	connections            map[string]app.Connection
	connectionClosed       []app.Connection
	connectionLastAction   map[string]app.ConnectionAction
	connectionScroll       int
	connectionErr          error
	connectionFinished     bool
	listenerPortStatus     app.ListenerPortStatus
	listenerPortField      string
	listenerPortResult     string
	proxyLayer             string
	proxyStatus            app.ProxyConfigStatus
	proxyTarget            string
	proxyHost              string
	proxyPort              int
	daemonRestartProgress  app.DaemonRestartProgress
	daemonRestartSeen      map[app.DaemonRestartPhase]bool
	rulesetStatus          app.RuleSetStatus
	rulesetErr             error
	rulesetPhase           string
	rulesetCancel          context.CancelFunc
}

type actionDoneMsg struct {
	result string
	err    error
}

type modeStatusMsg struct {
	status app.RoutingModeStatus
	err    error
	set    bool
}

type listenerPortsMsg struct {
	status app.ListenerPortStatus
	err    error
	set    bool
}

type proxyStatusMsg struct {
	status app.ProxyConfigStatus
	err    error
	set    bool
}

type groupsLoadedMsg struct {
	groups []app.Group
	err    error
}

type groupLoadedMsg struct {
	group app.Group
	err   error
}

type whitelistLoadedMsg struct {
	items  []string
	result string
	err    error
}

type routeDiagnosisMsg struct {
	value app.RouteDiagnosis
	err   error
}

type subscriptionLoadedMsg struct {
	value app.Subscription
	err   error
}

type coreStatusMsg struct {
	value app.CoreStatus
	err   error
}

type nodeSelectedMsg struct {
	node string
	err  error
}

type nodeTestStartedMsg struct {
	generation uint64
	ch         <-chan app.NodeTestEvent
}

type logStreamStartedMsg struct{ ch <-chan app.LogEvent }

type connectionStreamStartedMsg struct{ ch <-chan app.ConnectionEvent }

type nodeTestMsg struct {
	ch    <-chan app.NodeTestEvent
	event app.NodeTestEvent
	ok    bool
}

type switchNodeTestMsg struct {
	generation uint64
	ch         <-chan app.NodeTestEvent
	event      app.NodeTestEvent
	ok         bool
}

type logEventMsg struct {
	ch    <-chan app.LogEvent
	event app.LogEvent
	ok    bool
}

type connectionEventMsg struct {
	ch    <-chan app.ConnectionEvent
	event app.ConnectionEvent
	ok    bool
}

type daemonRestartStartedMsg struct{ ch <-chan daemonRestartEvent }

type daemonRestartEvent struct {
	ch       <-chan daemonRestartEvent
	progress *app.DaemonRestartProgress
	result   app.DaemonRestartResult
	err      error
	done     bool
}

type rulesetStatusMsg struct {
	status  app.RuleSetStatus
	err     error
	install bool
}

type rulesetInstallStartedMsg struct {
	ch <-chan app.RuleSetInstallEvent
}

type rulesetInstallEventMsg struct {
	ch    <-chan app.RuleSetInstallEvent
	event app.RuleSetInstallEvent
	ok    bool
}

func New(client Capabilities) Model {
	return NewWithContext(context.Background(), client)
}

func NewWithContext(ctx context.Context, client Capabilities) Model {
	if ctx == nil {
		ctx = context.Background()
	}
	ti := textinput.New()
	ti.Prompt = "> "
	ti.CharLimit = 200
	ti.Width = 70
	return Model{client: client, ctx: ctx, page: mainMenu, returnPage: actionMenu, input: ti, now: time.Now, mainItems: []string{"运行模式", "实时连接", "服务管理", "节点管理", "订阅管理", "白名单管理", "路由诊断", "配置管理", "退出"}}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case daemonRestartStartedMsg:
		return m, waitDaemonRestartEventMsg(msg.ch)
	case daemonRestartEvent:
		if msg.progress != nil {
			m.daemonRestartProgress = *msg.progress
			if m.daemonRestartSeen == nil {
				m.daemonRestartSeen = map[app.DaemonRestartPhase]bool{}
			}
			m.daemonRestartSeen[msg.progress.Phase] = true
			return m, waitDaemonRestartEventMsg(msg.ch)
		}
		if msg.done {
			m.busy = false
			m.actionCtx = ""
			m.actionItems = menuActions("服务管理")
			m.actionIndex = 4
			m.result = formatDaemonRestartResult(msg.result, msg.err)
			m.err = msg.err
			m.returnPage = actionMenu
			m.page = resultView
			return m, nil
		}
		return m, nil
	case rulesetInstallStartedMsg:
		return m, waitRulesetInstallEventMsg(msg.ch)
	case rulesetInstallEventMsg:
		if !msg.ok {
			m.busy = false
			m.rulesetCancel = nil
			m.rulesetErr = fmt.Errorf("规则集安装流意外结束")
			return m, nil
		}
		if msg.event.Phase != "" {
			m.rulesetPhase = msg.event.Phase
		}
		if msg.event.Status != nil {
			m.rulesetStatus = *msg.event.Status
		}
		if msg.event.Err != nil {
			m.busy = false
			m.rulesetCancel = nil
			m.rulesetErr = msg.event.Err
			return m, nil
		}
		if msg.event.Done {
			m.busy = false
			m.rulesetCancel = nil
			return m, nil
		}
		return m, waitRulesetInstallEventMsg(msg.ch)
	case rulesetStatusMsg:
		m.busy = false
		m.rulesetCancel = nil
		m.rulesetErr = msg.err
		if msg.err == nil {
			m.rulesetStatus = msg.status
		}
		if msg.install && msg.err != nil {
			m.result, m.err, m.returnPage, m.page = "CN 规则集安装失败", msg.err, actionMenu, resultView
		}
		return m, nil
	case modeStatusMsg:
		m.busy = false
		m.modeCancel = nil
		m.modeErr = msg.err
		if msg.err == nil || msg.status.ConfigMode != "" {
			m.modeStatus = msg.status
			if msg.status.ConfigMode != "" {
				m.modeSelected = msg.status.ConfigMode
			}
		}
		if msg.set {
			if msg.err == nil {
				if msg.status.NextStart {
					m.modeResult = "配置已保存，将在下次启动 mihomo 时生效"
				} else {
					m.modeResult = "运行模式已切换"
				}
			} else {
				m.modeResult = modeErrorMessage(msg.err)
			}
		}
		return m, nil
	case listenerPortsMsg:
		m.busy = false
		if msg.status.ProfileID != "" {
			m.listenerPortStatus = msg.status
			m.actionItems = listenerPortActionItems(msg.status.Ports)
		}
		m.actionCtx = "listener_ports"
		if msg.err != nil {
			m.result = "监听端口操作失败"
			m.err = msg.err
			m.returnPage = actionMenu
			m.page = resultView
			return m, nil
		}
		m.page = actionMenu
		if msg.set {
			if msg.status.Restarted {
				m.listenerPortResult = "端口已修改，mihomo 已重启并生效"
			} else if msg.status.NextStart {
				m.listenerPortResult = "端口已保存，将在下次启动 mihomo 时生效"
			} else {
				m.listenerPortResult = "端口已修改"
			}
		}
		return m, nil
	case proxyStatusMsg:
		m.busy = false
		if msg.err != nil {
			m.result, m.err, m.returnPage, m.page = "代理配置操作失败", msg.err, actionMenu, resultView
			return m, nil
		}
		m.proxyStatus = msg.status
		m.proxyLayer = msg.status.Layer
		m.actionCtx = "proxy_" + msg.status.Layer
		m.actionItems = proxyActionItems(msg.status.Layer)
		m.actionIndex = 0
		m.page = actionMenu
		if msg.set {
			m.result = "代理配置已更新"
			if msg.status.Layer == "env" {
				m.result += "\n新终端自动生效；当前终端请执行 source ~/.bashrc"
			}
			m.err, m.returnPage, m.page = nil, actionMenu, resultView
		}
		return m, nil
	case groupsLoadedMsg:
		m.busy = false
		if msg.err != nil {
			m.result, m.err, m.returnPage, m.page = "读取代理组失败", msg.err, mainMenu, resultView
			return m, nil
		}
		if len(msg.groups) == 0 {
			m.result, m.err, m.returnPage, m.page = "没有可管理的代理组", nil, mainMenu, resultView
			return m, nil
		}
		m.groups = msg.groups
		m.actionCtx = "group_list"
		m.actionItems = formatGroupItems(msg.groups)
		m.actionIndex = 0
		m.page = actionMenu
		return m, nil
	case groupLoadedMsg:
		m.busy = false
		if msg.err != nil {
			m.result, m.err, m.returnPage, m.page = "读取代理组节点失败", msg.err, actionMenu, resultView
			return m, nil
		}
		return m.enterLoadedGroup(msg.group)
	case whitelistLoadedMsg:
		m.busy = false
		if msg.err != nil {
			result := "读取白名单失败"
			returnPage := mainMenu
			if msg.result != "" {
				result = msg.result + "失败"
				returnPage = actionMenu
			}
			m.result, m.err, m.returnPage, m.page = result, msg.err, returnPage, resultView
			return m, nil
		}
		m.whitelistItems = sortUniqueDomains(msg.items)
		m.actionCtx = "whitelist_list"
		m.actionItems = m.whitelistActionItems()
		m.actionIndex = 0
		if msg.result != "" {
			m.result, m.err, m.returnPage, m.page = msg.result, nil, actionMenu, resultView
		} else {
			m.page = actionMenu
		}
		return m, nil
	case routeDiagnosisMsg:
		m.busy = false
		if msg.err != nil {
			m.result, m.err, m.returnPage, m.page = "诊断失败", msg.err, mainMenu, resultView
			return m, nil
		}
		m.result = formatRouteDiagnosis(msg.value)
		m.err = nil
		m.returnPage = mainMenu
		m.page = resultView
		return m, nil
	case subscriptionLoadedMsg:
		m.busy = false
		if msg.err != nil {
			m.result, m.err, m.returnPage, m.page = "读取订阅失败", msg.err, actionMenu, resultView
			return m, nil
		}
		m.result = "当前 URL: " + redactURL(msg.value.URL)
		m.err = nil
		m.returnPage = actionMenu
		m.page = resultView
		return m, nil
	case coreStatusMsg:
		m.busy = false
		if msg.err != nil {
			m.result, m.err = "读取服务状态失败", msg.err
		} else {
			m.result = fmt.Sprintf("服务状态: %s\n档案: %s\nPID: %d", coreStateLabel(msg.value.State), msg.value.ProfileID, msg.value.PID)
			m.err = nil
		}
		m.returnPage = actionMenu
		m.page = resultView
		return m, nil
	case nodeSelectedMsg:
		m.busy = false
		if msg.err != nil {
			m.result, m.err, m.returnPage, m.page = "节点切换失败", msg.err, actionMenu, resultView
			return m, nil
		}
		m.currentNodeName = msg.node
		for index := range m.groups {
			if m.groups[index].ID.String() == m.currentGroupID {
				m.groups[index].SelectedNodeID = domain.NodeID(msg.node)
			}
		}
		m.switchTestMessage = "节点已切换"
		return m, nil
	case nodeTestStartedMsg:
		if msg.generation != m.switchTestGeneration || m.nodeTestMode == nodeTestIdle {
			return m, nil
		}
		return m, waitSwitchNodeTestMsg(msg.ch, msg.generation)
	case logStreamStartedMsg:
		return m, waitLogEventMsg(msg.ch)
	case connectionStreamStartedMsg:
		return m, waitConnectionEventMsg(msg.ch)
	case actionDoneMsg:
		m.busy = false
		m.result = msg.result
		m.err = msg.err
		if isPortConflict(msg.err) {
			m.result += "\n请进入 配置管理 > 监听端口 修改冲突端口"
		}
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
				b.WriteString(fmt.Sprintf("%d. %s - %dms\n", i+1, m.liveResults[i].NodeID, m.liveResults[i].Delay.Milliseconds()))
			}
			m.result = b.String()
			m.returnPage = actionMenu
			m.page = resultView
			return m, nil
		}
		return m, waitNodeTestMsg(msg.ch)
	case switchNodeTestMsg:
		if msg.generation != m.switchTestGeneration || m.nodeTestMode == nodeTestIdle || (m.actionCtx != "switch_nodes" && m.actionCtx != "group_nodes") {
			return m, nil
		}
		if !msg.ok {
			return m.finishNodeTestStream("测速流已结束")
		}
		if msg.event.Err != nil {
			message := ""
			if !errors.Is(msg.event.Err, context.Canceled) {
				message = "测速失败: " + msg.event.Err.Error()
			}
			m.cancelNodeTest(message)
			return m, nil
		}
		if msg.event.Result != nil {
			if m.nodeResults == nil {
				m.nodeResults = map[string]app.NodeDelay{}
			}
			result := *msg.event.Result
			if result.TestedAt.IsZero() {
				result.TestedAt = m.clockNow()
			}
			m.nodeResults[result.NodeID.String()] = result
		}
		m.switchTestDone = msg.event.Done
		m.switchTestTotal = msg.event.Total
		if msg.event.Finished {
			return m.finishNodeTestStream(fmt.Sprintf("测速完成 %d/%d", msg.event.Done, msg.event.Total))
		}
		return m, waitSwitchNodeTestMsg(msg.ch, msg.generation)
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
		if msg.event.Line != nil {
			m.appendLogLine(msg.event.Line.Message)
		}
		return m, waitLogEventMsg(msg.ch)
	case connectionEventMsg:
		if m.actionCtx != "connections_live" {
			return m, nil
		}
		if !msg.ok {
			m.connectionFinished = true
			m.cancelConnectionFollow()
			return m, nil
		}
		if msg.event.Err != nil {
			if !errors.Is(msg.event.Err, context.Canceled) {
				m.connectionErr = msg.event.Err
			}
			m.connectionFinished = true
			m.cancelConnectionFollow()
			return m, nil
		}
		if msg.event.Finished {
			m.connectionFinished = true
			m.cancelConnectionFollow()
			return m, nil
		}
		m.reduceConnectionEvent(msg.event)
		return m, waitConnectionEventMsg(msg.ch)
	case tea.KeyMsg:
		s := msg.String()
		if m.actionCtx == "daemon_restart_progress" && m.busy {
			return m, nil
		}
		if m.actionCtx == "daemon_restart_confirm" {
			switch s {
			case "esc":
				m.actionCtx = ""
				m.actionItems = menuActions("服务管理")
				m.actionIndex = 4
				return m, nil
			case "enter":
				m.actionCtx = "daemon_restart_progress"
				m.busy = true
				m.daemonRestartProgress = app.DaemonRestartProgress{Phase: app.DaemonRestartPhasePreflight, Step: 1, Total: 6, Message: "正在启动组合重启"}
				m.daemonRestartSeen = map[app.DaemonRestartPhase]bool{}
				return m, startDaemonRestartCmd(context.WithoutCancel(m.ctx), m.client)
			default:
				return m, nil
			}
		}
		if s == "ctrl+c" {
			if m.actionCtx == "ruleset" && m.busy && m.rulesetCancel != nil && m.rulesetPhase != "publishing" && m.rulesetPhase != "reloading" && m.rulesetPhase != "verifying" {
				m.rulesetCancel()
				m.rulesetCancel = nil
				m.busy = false
				m.rulesetErr = context.Canceled
				return m, nil
			}
			return m, tea.Quit
		}
		if m.busy {
			if s == "esc" && m.actionCtx == "ruleset" && m.rulesetCancel != nil && m.rulesetPhase != "publishing" && m.rulesetPhase != "reloading" && m.rulesetPhase != "verifying" {
				m.rulesetCancel()
				m.rulesetCancel = nil
				m.busy = false
				m.rulesetErr = context.Canceled
				return m, nil
			}
			if s == "esc" && m.actionCtx == "mode" {
				if m.modeCancel != nil {
					m.modeCancel()
					m.modeCancel = nil
				}
				m.busy = false
				m.actionCtx = ""
				m.page = mainMenu
				m.modeErr = nil
				m.modeResult = ""
			}
			return m, nil
		}
		switch s {
		case "q":
			if m.page == mainMenu {
				return m, tea.Quit
			}
		case "esc":
			if m.page == actionMenu && m.actionCtx == "mode" {
				if m.modeCancel != nil {
					m.modeCancel()
					m.modeCancel = nil
				}
				m.actionCtx = ""
				m.actionItems = nil
				m.actionIndex = 0
				m.page = mainMenu
				m.modeResult = ""
				m.modeErr = nil
				return m, nil
			}
			if m.actionCtx == "log_live" {
				if m.logFollowCancel != nil {
					m.logFollowCancel()
					m.logFollowCancel = nil
				}
				m.actionCtx = ""
				m.actionItems = menuActions("服务管理")
				m.actionIndex = 0
				return m, nil
			}
			if m.actionCtx == "connections_live" {
				if m.connectionFollowCancel != nil {
					m.connectionFollowCancel()
					m.connectionFollowCancel = nil
				}
				m.actionCtx = ""
				m.actionItems = nil
				m.page = mainMenu
				return m, nil
			}
			if m.page == actionMenu && (m.actionCtx == "switch_nodes" || m.actionCtx == "group_nodes") {
				if m.nodeTestMode != nodeTestIdle {
					m.cancelNodeTest("测速已取消")
					return m, nil
				}
				m.cancelNodeTest("")
				if m.actionCtx == "group_nodes" {
					m.actionCtx = "group_list"
					m.actionItems = formatGroupItems(m.groups)
				} else {
					m.actionCtx = ""
					m.actionItems = menuActions("节点管理")
				}
				m.actionIndex = 0
				m.nodeResults = nil
				m.nodeTestable = nil
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
			if m.page == actionMenu && m.actionCtx == "listener_ports" {
				m.actionCtx = ""
				m.actionItems = menuActions("配置管理")
				m.actionIndex = 0
				m.listenerPortResult = ""
				return m, nil
			}
			if m.page == actionMenu && (m.actionCtx == "ruleset" || m.actionCtx == "ruleset_confirm") {
				m.actionCtx = ""
				m.actionItems = menuActions("配置管理")
				m.actionIndex = 0
				m.rulesetErr = nil
				m.rulesetPhase = ""
				return m, nil
			}
			if m.page == actionMenu && strings.HasPrefix(m.actionCtx, "proxy_") {
				if m.actionCtx == "proxy_confirm" {
					m.actionCtx = "proxy_" + m.proxyLayer
					m.actionItems = proxyActionItems(m.proxyLayer)
					m.actionIndex = 0
					return m, nil
				}
				m.actionCtx = ""
				m.actionItems = menuActions("配置管理")
				m.actionIndex = 0
				return m, nil
			}
			if m.page == actionMenu || m.page == resultView || m.page == inputView {
				if m.page == inputView {
					if m.actionCtx == "listener_port_input" {
						m.input.Blur()
						m.actionCtx = "listener_ports"
						m.page = actionMenu
						return m, nil
					}
					if m.actionCtx == "proxy_input" {
						m.input.Blur()
						m.actionCtx = "proxy_" + m.proxyLayer
						m.actionItems = proxyActionItems(m.proxyLayer)
						m.page = actionMenu
						return m, nil
					}
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
		if m.actionCtx == "connections_live" {
			switch s {
			case "up":
				if m.connectionScroll > 0 {
					m.connectionScroll--
				}
			case "down":
				if m.connectionScroll < max(0, len(m.connections)-1) {
					m.connectionScroll++
				}
			}
			return m, nil
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
		if choice == "运行模式" {
			m.actionCtx = "mode"
			m.actionItems = modeActionItems()
			m.actionIndex = 0
			m.modeResult = ""
			m.modeErr = nil
			m.page = actionMenu
			m.busy = true
			ctx, cancel := context.WithCancel(m.ctx)
			m.modeCancel = cancel
			return m, loadModeStatusCmd(ctx, m.client)
		}
		if choice == "实时连接" {
			m.connections = map[string]app.Connection{}
			m.connectionClosed = nil
			m.connectionLastAction = map[string]app.ConnectionAction{}
			m.connectionScroll = 0
			m.connectionErr = nil
			m.connectionFinished = false
			if m.connectionFollowCancel != nil {
				m.connectionFollowCancel()
			}
			ctx, cancel := context.WithCancel(m.ctx)
			m.connectionFollowCancel = cancel
			m.actionCtx = "connections_live"
			m.actionItems = nil
			m.page = actionMenu
			return m, startConnectionFollowCmd(ctx, m.client)
		}
		if choice == "节点管理" {
			m.busy = true
			return m, loadGroupsCmd(m.ctx, m.client)
		}
		if choice == "白名单管理" {
			m.busy = true
			return m, loadWhitelistCmd(m.ctx, m.client, "")
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
	if m.actionCtx == "mode" {
		return m.updateMode(msg)
	}
	if m.actionCtx == "listener_ports" {
		return m.updateListenerPorts(msg)
	}
	if m.actionCtx == "ruleset" || m.actionCtx == "ruleset_confirm" {
		return m.updateRuleSet(msg)
	}
	if m.actionCtx == "proxy_system" || m.actionCtx == "proxy_env" || m.actionCtx == "proxy_confirm" {
		return m.updateProxy(msg)
	}
	switch s {
	case "a":
		if m.actionCtx == "group_nodes" {
			if m.testableNodeCount() == 0 {
				m.switchTestMessage = "当前代理组没有可测速节点"
				return m, nil
			}
			m.nodeTestQueue = nil
			return m.startNodeTest(app.NodeTestRequest{GroupID: domain.GroupID(m.currentGroupID), Concurrency: 5, Limit: 0}, nodeTestBatch)
		}
		if m.actionCtx == "whitelist_list" {
			m.inputTitle = "新增白名单域名"
			m.inputDo = func(v string) tea.Cmd {
				return mutateWhitelistCmd(m.ctx, m.client, "add", "", v)
			}
			m.input.SetValue("")
			m.input.Focus()
			m.page = inputView
			return m, textinput.Blink
		}
	case "t":
		if m.actionCtx == "group_nodes" {
			if len(m.actionItems) == 0 {
				return m, nil
			}
			node := m.actionItems[m.actionIndex]
			if node == "返回" {
				m.switchTestMessage = "请先选择一个节点"
				return m, nil
			}
			if !m.nodeTestable[node] {
				m.switchTestMessage = "该节点不可测速"
				return m, nil
			}
			nodeID := domain.NodeID(node)
			if m.nodeTestMode == nodeTestSingle {
				if nodeID == m.nodeTestCurrent {
					m.switchTestMessage = "该节点正在测速"
					return m, nil
				}
				if m.nodeTestQueued(nodeID) {
					m.switchTestMessage = "该节点已在队列中"
					return m, nil
				}
				m.nodeTestQueue = append(m.nodeTestQueue, nodeID)
				m.switchTestMessage = "已加入队列: " + node
				return m, nil
			}
			m.nodeTestQueue = nil
			return m.startNodeTest(app.NodeTestRequest{GroupID: domain.GroupID(m.currentGroupID), NodeID: nodeID}, nodeTestSingle)
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
				m.cancelNodeTest("")
				m.actionCtx = ""
				m.actionItems = menuActions("节点管理")
				m.actionIndex = 0
				m.nodeResults = nil
				m.nodeTestable = nil
				m.switchTestDone = 0
				m.switchTestTotal = 0
				return m, nil
			}
			m.cancelNodeTest("")
			m.actionCtx = ""
			m.actionItems = menuActions("节点管理")
			m.actionIndex = 0
			m.nodeResults = nil
			m.nodeTestable = nil
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
			groupID := parseGroupName(act)
			for _, group := range m.groups {
				if group.Name == groupID {
					groupID = group.ID.String()
					break
				}
			}
			m.busy = true
			return m, loadGroupCmd(m.ctx, m.client, groupID)
		}
		if m.actionCtx == "group_nodes" {
			if act == "返回" {
				m.cancelNodeTest("")
				m.actionCtx = "group_list"
				m.actionItems = formatGroupItems(m.groups)
				m.actionIndex = 0
				m.nodeResults = nil
				m.nodeTestable = nil
				m.switchTestDone = 0
				m.switchTestTotal = 0
				return m, nil
			}
			m.busy = true
			return m, selectNodeCmd(m.ctx, m.client, m.currentGroupID, act)
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
				m.busy = true
				return m, mutateWhitelistCmd(m.ctx, m.client, "remove", m.selectedWhitelist, "")
			case "修改":
				old := m.selectedWhitelist
				m.inputTitle = "修改白名单域名"
				m.inputDo = func(v string) tea.Cmd {
					return mutateWhitelistCmd(m.ctx, m.client, "edit", old, v)
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
	if msg.String() == "enter" && m.actionCtx == "proxy_input" {
		parts := strings.Fields(strings.TrimSpace(m.input.Value()))
		if len(parts) != 1 && len(parts) != 3 {
			m.result = "请输入 target，或同时输入 target、IP 和端口"
			m.err = nil
			m.returnPage = actionMenu
			m.page = resultView
			m.input.Blur()
			return m, nil
		}
		m.proxyTarget, m.proxyHost, m.proxyPort = strings.ToLower(parts[0]), "", 0
		if len(parts) == 3 {
			port, err := strconv.Atoi(parts[2])
			if err != nil || net.ParseIP(strings.Trim(parts[1], "[]")) == nil || port < 1 || port > 65535 {
				m.result = "IP 必须是 IPv4/IPv6 字面量，端口范围为 1-65535"
				m.err = nil
				m.returnPage = actionMenu
				m.page = resultView
				m.input.Blur()
				return m, nil
			}
			m.proxyHost, m.proxyPort = strings.Trim(parts[1], "[]"), port
		}
		if m.proxyTarget != "http" && m.proxyTarget != "https" && m.proxyTarget != "socks" && m.proxyTarget != "all" {
			m.result = "代理类型必须为 http、https、socks 或 all"
			m.err = nil
			m.returnPage = actionMenu
			m.page = resultView
			m.input.Blur()
			return m, nil
		}
		m.input.SetValue("")
		m.input.Blur()
		m.actionCtx = "proxy_confirm"
		m.actionItems = []string{"确认应用", "取消"}
		m.actionIndex = 0
		m.page = actionMenu
		return m, nil
	}
	if msg.String() == "enter" && m.actionCtx == "listener_port_input" {
		value := strings.TrimSpace(m.input.Value())
		port, err := strconv.Atoi(value)
		request := app.SetListenerPortRequest{Field: m.listenerPortField, Port: port}
		if err != nil || request.Validate() != nil {
			m.input.Blur()
			m.actionCtx = "listener_ports"
			m.result = "端口必须是允许范围内的整数"
			m.err = nil
			m.returnPage = actionMenu
			m.page = resultView
			return m, nil
		}
		m.input.SetValue("")
		m.input.Blur()
		m.actionCtx = "listener_ports"
		m.busy = true
		return m, setListenerPortCmd(m.ctx, m.client, request)
	}
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
		m.busy = true
		return m, diagnoseRouteCmd(m.ctx, m.client, val)
	}
	if msg.String() == "enter" && m.inputDo != nil {
		run := m.inputDo
		value := m.input.Value()
		m.inputDo = nil
		m.input.SetValue("")
		m.input.Blur()
		m.busy = true
		return m, run(value)
	}
	return m, cmd
}

func (m Model) updateListenerPorts(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.actionItems) == 0 {
		return m, nil
	}
	switch msg.String() {
	case "up":
		if m.actionIndex > 0 {
			m.actionIndex--
		}
	case "down":
		if m.actionIndex < len(m.actionItems)-1 {
			m.actionIndex++
		}
	case "enter":
		field := m.actionItems[m.actionIndex]
		if field == "返回" {
			m.actionCtx = ""
			m.actionItems = menuActions("配置管理")
			m.actionIndex = 0
			m.listenerPortResult = ""
			return m, nil
		}
		for _, item := range m.listenerPortStatus.Ports {
			if item.Field != field {
				continue
			}
			m.listenerPortField = field
			m.inputTitle = fmt.Sprintf("设置 %s 端口（0 表示禁用可选端口）", field)
			m.input.SetValue(strconv.Itoa(item.Port))
			m.input.Focus()
			m.actionCtx = "listener_port_input"
			m.page = inputView
			return m, textinput.Blink
		}
	}
	return m, nil
}

func (m Model) updateProxy(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.actionItems) == 0 {
		return m, nil
	}
	switch msg.String() {
	case "up":
		if m.actionIndex > 0 {
			m.actionIndex--
		}
	case "down":
		if m.actionIndex < len(m.actionItems)-1 {
			m.actionIndex++
		}
	case "enter":
		act := m.actionItems[m.actionIndex]
		if m.actionCtx == "proxy_confirm" {
			if (m.proxyTarget == "restore" && act != "确认恢复") || (m.proxyTarget == "disable" && act != "确认禁用") || (m.proxyTarget != "restore" && m.proxyTarget != "disable" && act != "确认应用") {
				m.actionCtx = "proxy_" + m.proxyLayer
				m.actionItems = proxyActionItems(m.proxyLayer)
				m.actionIndex = 0
				return m, nil
			}
			m.busy = true
			if m.proxyTarget == "restore" {
				return m, restoreProxyCmd(m.ctx, m.client, m.proxyLayer)
			}
			if m.proxyTarget == "disable" {
				return m, disableProxyCmd(m.ctx, m.client, m.proxyLayer)
			}
			return m, setProxyCmd(m.ctx, m.client, app.ProxyRequest{Layer: m.proxyLayer, Target: m.proxyTarget, Host: m.proxyHost, Port: m.proxyPort})
		}
		switch act {
		case "查看状态":
			m.busy = true
			return m, loadProxyStatusCmd(m.ctx, m.client, m.proxyLayer)
		case "设置代理":
			m.inputTitle = "输入 target [IP 端口]（省略 IP/端口使用当前 listener）"
			m.input.SetValue("")
			m.input.Focus()
			m.actionCtx = "proxy_input"
			m.page = inputView
			return m, textinput.Blink
		case "恢复启用前设置":
			m.actionCtx = "proxy_confirm"
			m.actionItems = []string{"确认恢复", "取消"}
			m.actionIndex = 0
			m.proxyTarget = "restore"
			return m, nil
		case "禁用管理区块":
			m.actionCtx = "proxy_confirm"
			m.actionItems = []string{"确认禁用", "取消"}
			m.actionIndex = 0
			m.proxyTarget = "disable"
			return m, nil
		case "返回":
			m.actionCtx = ""
			m.actionItems = menuActions("配置管理")
			m.actionIndex = 0
			return m, nil
		}
	}
	return m, nil
}

func (m Model) updateRuleSet(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.actionItems) == 0 {
		return m, nil
	}
	switch msg.String() {
	case "up":
		if m.actionIndex > 0 {
			m.actionIndex--
		}
	case "down":
		if m.actionIndex < len(m.actionItems)-1 {
			m.actionIndex++
		}
	case "enter":
		if m.actionCtx == "ruleset_confirm" {
			if m.actionItems[m.actionIndex] != "确认安装" {
				m.actionCtx = "ruleset"
				m.actionItems = []string{"安装/修复", "返回"}
				m.actionIndex = 0
				return m, nil
			}
			m.actionCtx = "ruleset"
			m.actionItems = []string{"安装/修复", "返回"}
			m.actionIndex = 0
			m.busy = true
			m.rulesetPhase = "checking"
			m.rulesetErr = nil
			ctx, cancel := context.WithCancel(m.ctx)
			m.rulesetCancel = cancel
			return m, installRuleSetsCmd(ctx, m.client)
		}
		if m.actionItems[m.actionIndex] == "安装/修复" {
			m.actionCtx = "ruleset_confirm"
			m.actionItems = []string{"确认安装", "取消"}
			m.actionIndex = 0
			return m, nil
		}
		if m.actionItems[m.actionIndex] == "返回" {
			m.actionCtx = ""
			m.actionItems = menuActions("配置管理")
			m.actionIndex = 1
			return m, nil
		}
		if _, ok := m.client.(app.RuleSetCapability); !ok {
			m.rulesetErr = fmt.Errorf("当前 daemon 不支持 CN 规则集管理")
			return m, nil
		}
		return m, nil
	}
	return m, nil
}

func (m Model) executeAction(cat, act string) (tea.Model, tea.Cmd) {
	switch cat {
	case "服务管理":
		switch act {
		case "状态":
			m.busy = true
			return m, coreStatusCmd(m.ctx, m.client)
		case "启动":
			m.busy = true
			return m, actionCmd("已启动", func() error { return m.client.CoreAction(m.ctx, "", "start") })
		case "停止":
			m.busy = true
			return m, actionCmd("已停止", func() error { return m.client.CoreAction(m.ctx, "", "stop") })
		case "重启 Core":
			m.busy = true
			return m, actionCmd("Core 已重启", func() error { return m.client.CoreAction(m.ctx, "", "restart") })
		case "重启全部":
			m.actionCtx = "daemon_restart_confirm"
			m.actionItems = nil
			m.actionIndex = 0
			m.page = actionMenu
			return m, nil
		case "热重载":
			m.busy = true
			return m, actionCmd("已热重载", func() error { return m.client.CoreAction(m.ctx, "", "reload") })
		case "配置测试":
			m.busy = true
			return m, actionCmd("配置测试通过", func() error { return m.client.ValidateConfig(m.ctx, "") })
		case "查看实时日志":
			m.logLines = nil
			m.logRawLines = nil
			m.logFilter = ""
			m.logRegex = nil
			m.logScrollOffset = 0
			if m.logFollowCancel != nil {
				m.logFollowCancel()
			}
			ctx, cancel := context.WithCancel(m.ctx)
			m.logFollowCancel = cancel
			m.actionCtx = "log_live"
			m.page = actionMenu
			return m, startLogFollowCmd(ctx, m.client)
		}
	case "节点管理":
		return m, nil
	case "订阅管理":
		switch act {
		case "保存订阅URL":
			m.inputTitle = "输入订阅 URL"
			m.inputDo = func(v string) tea.Cmd {
				return actionCmd("订阅 URL 已保存", func() error { return m.client.SetSubscription(m.ctx, "", strings.TrimSpace(v)) })
			}
			m.input.SetValue("")
			m.input.Focus()
			m.page = inputView
			return m, textinput.Blink
		case "查看订阅URL":
			m.busy = true
			return m, loadSubscriptionCmd(m.ctx, m.client)
		case "更新订阅":
			m.busy = true
			return m, actionCmd("订阅更新完成", func() error { return m.client.UpdateSubscription(m.ctx, "") })
		}
	case "白名单管理":
		return m, nil
	case "配置管理":
		switch act {
		case "备份配置":
			m.busy = true
			return m, actionCmd("配置已备份", func() error { return m.client.ConfigBackup(m.ctx, "") })
		case "恢复配置":
			m.busy = true
			return m, actionCmd("配置已恢复", func() error { return m.client.ConfigRestore(m.ctx, "") })
		case "编辑配置":
			m.busy = true
			return m, actionCmd("编辑结束", func() error { return m.client.EditConfig(m.ctx, "") })
		case "监听端口":
			m.busy = true
			m.actionCtx = "listener_ports"
			m.actionItems = nil
			m.actionIndex = 0
			m.listenerPortResult = ""
			return m, loadListenerPortsCmd(m.ctx, m.client)
		case "CN 规则集":
			m.actionCtx = "ruleset"
			m.actionItems = []string{"安装/修复", "返回"}
			m.actionIndex = 0
			m.rulesetPhase = ""
			m.rulesetErr = nil
			m.busy = true
			return m, loadRuleSetStatusCmd(m.ctx, m.client)
		case "系统代理", "Bash 环境代理":
			m.proxyLayer = "system"
			if act == "Bash 环境代理" {
				m.proxyLayer = "env"
			}
			m.actionCtx = "proxy_" + m.proxyLayer
			m.actionItems = proxyActionItems(m.proxyLayer)
			m.actionIndex = 0
			m.busy = true
			return m, loadProxyStatusCmd(m.ctx, m.client, m.proxyLayer)
		case "应用分流规则（大陆直连/其他走GLOBAL）":
			m.busy = true
			return m, actionCmd("分流规则已应用", func() error { return m.client.ApplyRoutePreset(m.ctx, "", "cn") })
		}
	}
	m.result = "未实现操作"
	m.returnPage = actionMenu
	m.page = resultView
	return m, nil
}

func menuActions(main string) []string {
	switch main {
	case "服务管理":
		return []string{"状态", "启动", "停止", "重启 Core", "重启全部", "热重载", "配置测试", "查看实时日志", "返回"}
	case "节点管理":
		return []string{"返回"}
	case "订阅管理":
		return []string{"保存订阅URL", "查看订阅URL", "更新订阅", "返回"}
	case "白名单管理":
		return []string{"返回"}
	case "配置管理":
		return []string{"监听端口", "CN 规则集", "系统代理", "Bash 环境代理", "备份配置", "恢复配置", "编辑配置", "应用分流规则（大陆直连/其他走GLOBAL）", "返回"}
	default:
		return []string{"返回"}
	}
}

func proxyActionItems(layer string) []string {
	items := []string{"查看状态", "设置代理"}
	if layer == "system" {
		items = append(items, "恢复启用前设置")
	} else {
		items = append(items, "禁用管理区块")
	}
	return append(items, "返回")
}

func modeActionItems() []string {
	return []string{"全局代理", "规则分流", "全局直连", "关闭现有连接", "应用切换", "返回"}
}

func (m Model) updateMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up":
		if m.actionIndex > 0 {
			m.actionIndex--
		}
	case "down":
		if m.actionIndex < len(m.actionItems)-1 {
			m.actionIndex++
		}
	case " ":
		if m.actionIndex == 3 {
			m.modeClose = !m.modeClose
		}
	case "enter":
		switch m.actionIndex {
		case 0:
			m.modeSelected = domain.RoutingModeGlobal
		case 1:
			m.modeSelected = domain.RoutingModeRule
		case 2:
			m.modeSelected = domain.RoutingModeDirect
		case 3:
			m.modeClose = !m.modeClose
		case 4:
			if m.modeSelected == "" {
				m.modeSelected = m.modeStatus.ConfigMode
			}
			if err := m.modeSelected.Validate(); err != nil {
				m.modeErr = err
				m.modeResult = "请选择运行模式"
				return m, nil
			}
			m.busy = true
			m.modeResult = ""
			m.modeErr = nil
			ctx, cancel := context.WithCancel(m.ctx)
			m.modeCancel = cancel
			return m, setModeCmd(ctx, m.client, m.modeStatus.ProfileID, m.modeSelected, m.modeClose)
		case 5:
			m.actionCtx = ""
			m.page = mainMenu
			m.modeResult = ""
			m.modeErr = nil
		}
	}
	return m, nil
}

func loadModeStatusCmd(ctx context.Context, service Capabilities) tea.Cmd {
	return func() tea.Msg {
		status, err := service.ModeStatus(ctx, "")
		return modeStatusMsg{status: status, err: err}
	}
}

func setModeCmd(ctx context.Context, service Capabilities, profileID domain.ProfileID, mode domain.RoutingMode, closeConnections bool) tea.Cmd {
	return func() tea.Msg {
		status, err := service.SetMode(ctx, app.SetRoutingModeRequest{ProfileID: profileID, Mode: mode, CloseConnections: closeConnections})
		return modeStatusMsg{status: status, err: err, set: true}
	}
}

func loadGroupsCmd(ctx context.Context, service Capabilities) tea.Cmd {
	return func() tea.Msg {
		groups, err := service.Groups(ctx, "")
		return groupsLoadedMsg{groups: groups, err: err}
	}
}

func loadGroupCmd(ctx context.Context, service Capabilities, groupID string) tea.Cmd {
	return func() tea.Msg {
		group, err := service.Group(ctx, "", groupID)
		return groupLoadedMsg{group: group, err: err}
	}
}

func loadWhitelistCmd(ctx context.Context, service Capabilities, result string) tea.Cmd {
	return func() tea.Msg {
		items, err := service.Whitelist(ctx, "")
		return whitelistLoadedMsg{items: items, result: result, err: err}
	}
}

func mutateWhitelistCmd(ctx context.Context, service Capabilities, action, oldValue, value string) tea.Cmd {
	return func() tea.Msg {
		value = normalizeDomainText(value)
		var err error
		var result string
		switch action {
		case "add":
			if value == "" {
				err = fmt.Errorf("域名不能为空")
			} else {
				err = service.AddWhitelist(ctx, "", value)
			}
			result = "已新增"
		case "remove":
			err = service.RemoveWhitelist(ctx, "", oldValue)
			result = "已删除"
		case "edit":
			if value == "" {
				err = fmt.Errorf("域名不能为空")
			} else {
				err = service.EditWhitelist(ctx, "", oldValue, value)
			}
			result = "已修改"
		default:
			err = fmt.Errorf("未知白名单操作")
		}
		if err != nil {
			return whitelistLoadedMsg{result: result, err: err}
		}
		items, err := service.Whitelist(ctx, "")
		return whitelistLoadedMsg{items: items, result: result, err: err}
	}
}

func selectNodeCmd(ctx context.Context, service Capabilities, groupID, nodeID string) tea.Cmd {
	return func() tea.Msg {
		err := service.SelectGroupNode(ctx, "", groupID, nodeID)
		return nodeSelectedMsg{node: nodeID, err: err}
	}
}

func startNodeTestCmd(ctx context.Context, service Capabilities, request app.NodeTestRequest, generation uint64) tea.Cmd {
	return func() tea.Msg {
		return nodeTestStartedMsg{generation: generation, ch: service.TestNodes(ctx, request)}
	}
}

func startLogFollowCmd(ctx context.Context, service Capabilities) tea.Cmd {
	return func() tea.Msg {
		return logStreamStartedMsg{ch: service.FollowLogs(ctx, app.LogRequest{Lines: 50})}
	}
}

func startConnectionFollowCmd(ctx context.Context, service Capabilities) tea.Cmd {
	return func() tea.Msg {
		return connectionStreamStartedMsg{ch: service.FollowConnections(ctx, app.ConnectionRequest{})}
	}
}

func diagnoseRouteCmd(ctx context.Context, service Capabilities, input string) tea.Cmd {
	return func() tea.Msg {
		value, err := service.DiagnoseRoute(ctx, "", input)
		return routeDiagnosisMsg{value: value, err: err}
	}
}

func loadSubscriptionCmd(ctx context.Context, service Capabilities) tea.Cmd {
	return func() tea.Msg {
		value, err := service.Subscription(ctx, "")
		return subscriptionLoadedMsg{value: value, err: err}
	}
}

func coreStatusCmd(ctx context.Context, service Capabilities) tea.Cmd {
	return func() tea.Msg {
		value, err := service.CoreStatus(ctx, "")
		return coreStatusMsg{value: value, err: err}
	}
}

func loadListenerPortsCmd(ctx context.Context, service Capabilities) tea.Cmd {
	return func() tea.Msg {
		value, err := service.ListenerPorts(ctx, "")
		return listenerPortsMsg{status: value, err: err}
	}
}

func setListenerPortCmd(ctx context.Context, service Capabilities, request app.SetListenerPortRequest) tea.Cmd {
	return func() tea.Msg {
		value, err := service.SetListenerPort(ctx, request)
		return listenerPortsMsg{status: value, err: err, set: true}
	}
}

func loadProxyStatusCmd(ctx context.Context, service Capabilities, layer string) tea.Cmd {
	return func() tea.Msg {
		value, err := service.ProxyStatus(ctx, layer, "")
		return proxyStatusMsg{status: value, err: err}
	}
}

func loadRuleSetStatusCmd(ctx context.Context, service Capabilities) tea.Cmd {
	return func() tea.Msg {
		provider, ok := service.(app.RuleSetCapability)
		if !ok {
			return rulesetStatusMsg{err: fmt.Errorf("当前 daemon 不支持 CN 规则集管理")}
		}
		value, err := provider.RuleSetStatus(ctx, "")
		return rulesetStatusMsg{status: value, err: err}
	}
}

func installRuleSetsCmd(ctx context.Context, service Capabilities) tea.Cmd {
	return func() tea.Msg {
		provider, ok := service.(app.RuleSetCapability)
		if !ok {
			return rulesetStatusMsg{err: fmt.Errorf("当前 daemon 不支持 CN 规则集管理"), install: true}
		}
		return rulesetInstallStartedMsg{ch: provider.InstallRuleSets(ctx, "")}
	}
}

func setProxyCmd(ctx context.Context, service Capabilities, request app.ProxyRequest) tea.Cmd {
	return func() tea.Msg {
		value, err := service.SetProxy(ctx, request)
		return proxyStatusMsg{status: value, err: err, set: true}
	}
}

func restoreProxyCmd(ctx context.Context, service Capabilities, layer string) tea.Cmd {
	return func() tea.Msg {
		value, err := service.RestoreProxy(ctx, app.ProxyRequest{Layer: layer})
		return proxyStatusMsg{status: value, err: err, set: true}
	}
}

func disableProxyCmd(ctx context.Context, service Capabilities, layer string) tea.Cmd {
	return func() tea.Msg {
		value, err := service.DisableProxy(ctx, app.ProxyRequest{Layer: layer})
		return proxyStatusMsg{status: value, err: err, set: true}
	}
}

func startDaemonRestartCmd(ctx context.Context, service Capabilities) tea.Cmd {
	return func() tea.Msg {
		events := make(chan daemonRestartEvent, 16)
		go func() {
			defer close(events)
			result, err := service.RestartDaemon(ctx, func(progress app.DaemonRestartProgress) {
				value := progress
				events <- daemonRestartEvent{progress: &value}
			})
			events <- daemonRestartEvent{result: result, err: err, done: true}
		}()
		return daemonRestartStartedMsg{ch: events}
	}
}

func actionCmd(result string, run func() error) tea.Cmd {
	return func() tea.Msg { return actionDoneMsg{result: result, err: run()} }
}

func formatRouteDiagnosis(value app.RouteDiagnosis) string {
	var out strings.Builder
	out.WriteString(fmt.Sprintf("输入: %s\n", value.Input))
	out.WriteString(fmt.Sprintf("Host: %s\n", value.Host))
	if value.MatchedRule != "" {
		out.WriteString(fmt.Sprintf("命中规则: %s\n", value.MatchedRule))
	}
	if value.Target != "" {
		out.WriteString(fmt.Sprintf("目标组: %s\n", value.Target))
	}
	if value.CurrentNode != "" {
		out.WriteString(fmt.Sprintf("当前节点: %s\n", value.CurrentNode))
	}
	if value.Confidence != "" {
		out.WriteString(fmt.Sprintf("置信度: %s\n", value.Confidence))
	}
	if strings.TrimSpace(value.Note) != "" {
		out.WriteString(fmt.Sprintf("备注: %s\n", strings.TrimSpace(value.Note)))
	}
	return out.String()
}

func redactURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "[已隐藏]"
	}
	return parsed.Scheme + "://" + parsed.Host + "/redacted"
}

func coreStateLabel(state domain.CoreState) string {
	switch state {
	case domain.CoreStateRunning:
		return "运行中"
	case domain.CoreStateStopped:
		return "已停止"
	case domain.CoreStateFailed:
		return "失败"
	default:
		return state.String()
	}
}

func modeErrorMessage(err error) string {
	var appErr *app.Error
	if !errors.As(err, &appErr) {
		return "操作失败，请查看 daemon 状态"
	}
	switch appErr.Code {
	case app.ErrorCodeDaemonUnavailable:
		return "daemon 不可用，请执行 mm daemon status；必要时执行 mm daemon start"
	case app.ErrorCodeNotFound:
		return "尚未迁移 legacy 配置，请执行 mm migrate plan，然后执行 mm migrate apply"
	case app.ErrorCode("PROFILE_MODE_UNSUPPORTED"):
		return "当前档案不支持模式切换"
	case app.ErrorCode("MODE_RUNTIME_MISMATCH"):
		return "配置模式与运行模式不一致，请检查 mm core status"
	case app.ErrorCode("RESTORE_FAILED"):
		return "恢复失败，状态可能不确定，请检查 mm daemon status 和 mm core status"
	case app.ErrorCode("CONNECTION_CLOSE_FAILED"):
		return "模式已生效，但关闭现有连接失败"
	default:
		return appErr.Message
	}
}

func isPortConflict(err error) bool {
	var appErr *app.Error
	return errors.As(err, &appErr) && appErr.Code == app.ErrorCode("PORT_CONFLICT")
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
				b.WriteString(fmt.Sprintf("%d. %s - %dms\n", i+1, m.liveResults[i].NodeID, m.liveResults[i].Delay.Milliseconds()))
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
		if m.actionCtx == "connections_live" {
			header = "实时连接"
		}
		pageSize := m.pageSize()
		start, end = pageWindow(idx, len(items), pageSize)
	}
	if m.page == actionMenu && m.actionCtx == "log_live" {
		return m.renderLogView(title)
	}
	if m.page == actionMenu && m.actionCtx == "connections_live" {
		return m.renderConnectionsView(title)
	}
	if m.page == actionMenu && m.actionCtx == "mode" {
		return m.renderModeView(title)
	}
	if m.page == actionMenu && m.actionCtx == "listener_ports" {
		return m.renderListenerPortsView(title)
	}
	if m.page == actionMenu && (m.actionCtx == "ruleset" || m.actionCtx == "ruleset_confirm") {
		return m.renderRuleSetView(title)
	}
	if m.page == actionMenu && (m.actionCtx == "proxy_system" || m.actionCtx == "proxy_env" || m.actionCtx == "proxy_confirm") {
		return m.renderProxyView(title)
	}
	if m.page == actionMenu && m.actionCtx == "daemon_restart_confirm" {
		return m.renderDaemonRestartConfirm(title)
	}
	if m.page == actionMenu && m.actionCtx == "daemon_restart_progress" {
		return m.renderDaemonRestartProgress(title)
	}
	var b strings.Builder
	for i := start; i < end; i++ {
		it := items[i]
		if m.page == actionMenu && m.actionCtx == "group_nodes" {
			b.WriteString(m.renderNodeListRow(it, i == idx))
			b.WriteString("\n")
			continue
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
			navigation := fitFooter(fmt.Sprintf("↑/↓ 选择 Enter 切换 ←/→ 翻页 | 第 %d/%d 页", currentPage, max(1, totalPages)), m.width)
			action := "t 单测 | a 批量 | Esc 返回"
			switch m.nodeTestMode {
			case nodeTestSingle:
				action = fmt.Sprintf("单测 %s %d/%d | 待测 %d | t 入队 a 批量 Esc 取消", m.nodeTestCurrent, m.switchTestDone, m.switchTestTotal, len(m.nodeTestQueue))
			case nodeTestBatch:
				action = fmt.Sprintf("批量测速 %d/%d | t 单测 a 重测 Esc 取消", m.switchTestDone, m.switchTestTotal)
			}
			if m.switchTestMessage != "" {
				action += " | " + m.switchTestMessage
			}
			footer = navigation + "\n" + fitFooter(action, m.width)
		} else if m.actionCtx == "group_list" {
			footer = fmt.Sprintf("%s | ←/→ 翻页", footer)
		} else if m.actionCtx == "whitelist_list" {
			footer = fmt.Sprintf("%s | ←/→ 翻页 | a 新增", footer)
		}
	}
	if m.actionCtx != "group_nodes" {
		footer = fitFooter(footer, m.width)
	}
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

func waitNodeTestMsg(ch <-chan app.NodeTestEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		return nodeTestMsg{ch: ch, event: ev, ok: ok}
	}
}

func waitSwitchNodeTestMsg(ch <-chan app.NodeTestEvent, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		return switchNodeTestMsg{generation: generation, ch: ch, event: ev, ok: ok}
	}
}

func waitLogEventMsg(ch <-chan app.LogEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		return logEventMsg{ch: ch, event: ev, ok: ok}
	}
}

func waitConnectionEventMsg(ch <-chan app.ConnectionEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		return connectionEventMsg{ch: ch, event: event, ok: ok}
	}
}

func waitRulesetInstallEventMsg(ch <-chan app.RuleSetInstallEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		return rulesetInstallEventMsg{ch: ch, event: event, ok: ok}
	}
}

func waitDaemonRestartEventMsg(ch <-chan daemonRestartEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return daemonRestartEvent{done: true, err: &app.Error{Code: app.ErrorCodeInternal, Message: "组合重启进度流意外结束"}}
		}
		event.ch = ch
		return event
	}
}

func sortNodeDelays(in []app.NodeDelay) {
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
	status := "未测速"
	if result, ok := m.nodeResults[node]; ok {
		age := relativeTestTime(m.clockNow(), result.TestedAt)
		if result.Status == app.NodeTestStatusSuccess && result.Delay > 0 {
			status = fmt.Sprintf("%dms · %s", result.Delay.Milliseconds(), age)
		} else {
			status = "失败 · " + age
		}
	}
	label := node + suffix
	if m.width <= 0 {
		return fmt.Sprintf("%s  [%s]", label, status)
	}
	statusText := "[" + status + "]"
	available := m.width - lipgloss.Width(statusText) - 6
	if available < 1 {
		available = 1
	}
	label = truncateDisplayWidth(label, available)
	padding := strings.Repeat(" ", max(0, available-lipgloss.Width(label)))
	return label + padding + "  " + statusText
}

func (m Model) renderNodeListRow(node string, selected bool) string {
	cursor := "  "
	if selected {
		cursor = "> "
	}
	row := cursor + m.renderSwitchNodeItem(node)
	if !selected {
		return row
	}
	return nodeCursorStyle.Render(row)
}

func truncateDisplayWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	ellipsis := "..."
	budget := width - lipgloss.Width(ellipsis)
	if budget <= 0 {
		ellipsis = ""
		budget = width
	}
	var result strings.Builder
	for _, char := range value {
		candidate := result.String() + string(char)
		if lipgloss.Width(candidate) > budget {
			break
		}
		result.WriteRune(char)
	}
	return result.String() + ellipsis
}

func fitFooter(s string, width int) string {
	if width <= 0 {
		return s
	}
	maxw := width - 2
	if lipgloss.Width(s) <= maxw {
		return s
	}
	compact := strings.NewReplacer(
		"↑/↓ 选择 Enter 进入 Esc 返回 q 退出", "↑/↓ Enter Esc q",
		"第 ", "P",
		" 页", "",
		" | ←/→ 翻页 | ", " | ←/→ | ",
		"进度 ", "",
	).Replace(s)
	if lipgloss.Width(compact) <= maxw {
		return compact
	}
	return truncateDisplayWidth(compact, max(0, maxw))
}

func formatGroupItems(groups []app.Group) []string {
	out := make([]string, 0, len(groups)+1)
	for _, g := range groups {
		now := strings.TrimSpace(g.SelectedNodeID.String())
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

func (m Model) enterLoadedGroup(group app.Group) (tea.Model, tea.Cmd) {
	if len(group.NodeIDs) == 0 {
		m.result = "该代理组没有可选节点"
		m.err = nil
		m.returnPage = actionMenu
		m.page = resultView
		return m, nil
	}
	nodes := make([]string, 0, len(group.NodeIDs))
	for _, node := range group.NodeIDs {
		nodes = append(nodes, node.String())
	}
	m.currentGroupID = group.ID.String()
	m.currentGroupName = group.Name
	m.currentNodeName = strings.TrimSpace(group.SelectedNodeID.String())
	m.actionCtx = "group_nodes"
	m.page = actionMenu
	m.actionItems = append(nodes, "返回")
	m.actionIndex = 0
	m.cancelNodeTest("")
	m.nodeResults = map[string]app.NodeDelay{}
	m.nodeTestable = map[string]bool{}
	if len(group.NodeStates) == 0 {
		for _, node := range nodes {
			m.nodeTestable[node] = likelyTestableNode(node)
		}
	} else {
		for _, state := range group.NodeStates {
			key := state.NodeID.String()
			m.nodeTestable[key] = state.Testable
			if state.Latest != nil {
				m.nodeResults[key] = *state.Latest
			}
		}
	}
	m.switchTestDone = 0
	m.switchTestTotal = 0
	m.switchTestMessage = ""
	return m, nil
}

func (m Model) startNodeTest(request app.NodeTestRequest, mode nodeTestMode) (tea.Model, tea.Cmd) {
	m.switchTestGeneration++
	if m.switchTestCancel != nil {
		m.switchTestCancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.switchTestCancel = cancel
	m.nodeTestMode = mode
	m.nodeTestCurrent = request.NodeID
	m.switchTestDone = 0
	m.switchTestTotal = 0
	if mode == nodeTestBatch {
		m.switchTestTotal = m.testableNodeCount()
		m.switchTestMessage = "批量测速已开始"
	} else {
		m.switchTestMessage = "单测已开始"
	}
	return m, startNodeTestCmd(ctx, m.client, request, m.switchTestGeneration)
}

func (m *Model) cancelNodeTest(message string) {
	m.switchTestGeneration++
	if m.switchTestCancel != nil {
		m.switchTestCancel()
	}
	m.switchTestCancel = nil
	m.nodeTestMode = nodeTestIdle
	m.nodeTestCurrent = ""
	m.nodeTestQueue = nil
	m.switchTestMessage = message
}

func (m Model) finishNodeTestStream(message string) (tea.Model, tea.Cmd) {
	if m.nodeTestMode == nodeTestSingle && len(m.nodeTestQueue) > 0 {
		next := m.nodeTestQueue[0]
		m.nodeTestQueue = m.nodeTestQueue[1:]
		return m.startNodeTest(app.NodeTestRequest{GroupID: domain.GroupID(m.currentGroupID), NodeID: next}, nodeTestSingle)
	}
	if m.switchTestCancel != nil {
		m.switchTestCancel()
	}
	m.switchTestCancel = nil
	m.nodeTestMode = nodeTestIdle
	m.nodeTestCurrent = ""
	m.nodeTestQueue = nil
	m.switchTestMessage = message
	return m, nil
}

func (m Model) nodeTestQueued(nodeID domain.NodeID) bool {
	for _, queued := range m.nodeTestQueue {
		if queued == nodeID {
			return true
		}
	}
	return false
}

func (m Model) testableNodeCount() int {
	count := 0
	for _, node := range m.actionItems {
		if node != "返回" && m.nodeTestable[node] {
			count++
		}
	}
	return count
}

func (m Model) clockNow() time.Time {
	if m.now == nil {
		return time.Now()
	}
	return m.now()
}

func relativeTestTime(now, testedAt time.Time) string {
	if testedAt.IsZero() || testedAt.After(now) {
		return "刚刚"
	}
	age := now.Sub(testedAt)
	switch {
	case age < time.Minute:
		return "刚刚"
	case age < time.Hour:
		return fmt.Sprintf("%d分钟前", int(age/time.Minute))
	case age < 24*time.Hour:
		return fmt.Sprintf("%d小时前", int(age/time.Hour))
	case age < 30*24*time.Hour:
		return fmt.Sprintf("%d天前", int(age/(24*time.Hour)))
	default:
		return testedAt.Local().Format("2006-01-02")
	}
}

func likelyTestableNode(node string) bool {
	value := strings.TrimSpace(node)
	return value != "" && !strings.EqualFold(value, "DIRECT") && !strings.EqualFold(value, "REJECT") && !strings.HasPrefix(value, "官网") && !strings.HasPrefix(value, "有效期")
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

func (m *Model) reduceConnectionEvent(event app.ConnectionEvent) {
	if event.Connection == nil || strings.TrimSpace(event.Connection.ID) == "" {
		return
	}
	if m.connections == nil {
		m.connections = map[string]app.Connection{}
	}
	if m.connectionLastAction == nil {
		m.connectionLastAction = map[string]app.ConnectionAction{}
	}
	id := event.Connection.ID
	switch event.Action {
	case app.ConnectionActionOpen, app.ConnectionActionUpdate:
		m.connections[id] = *event.Connection
		m.connectionLastAction[id] = event.Action
	case app.ConnectionActionClosed:
		delete(m.connections, id)
		delete(m.connectionLastAction, id)
		m.connectionClosed = append([]app.Connection{*event.Connection}, m.connectionClosed...)
		const maxClosedNotices = 20
		if len(m.connectionClosed) > maxClosedNotices {
			m.connectionClosed = m.connectionClosed[:maxClosedNotices]
		}
	}
}

func (m *Model) cancelConnectionFollow() {
	if m.connectionFollowCancel != nil {
		m.connectionFollowCancel()
		m.connectionFollowCancel = nil
	}
}

func (m Model) renderConnectionsView(title string) string {
	ids := make([]string, 0, len(m.connections))
	for id := range m.connections {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	visible := max(3, m.height-9)
	if m.width < 96 {
		visible = max(2, visible/2)
	}
	maxScroll := max(0, len(ids)-visible)
	scroll := min(max(0, m.connectionScroll), maxScroll)
	start, end := scroll, min(len(ids), scroll+visible)
	var body strings.Builder
	if m.width >= 96 {
		body.WriteString(fmt.Sprintf("%-6s %-26s %-6s %-18s %-18s %10s %10s\n", "事件", "目标", "网络", "规则", "最终节点", "上传", "下载"))
		for _, id := range ids[start:end] {
			value := m.connections[id]
			body.WriteString(fmt.Sprintf("%-6s %-26s %-6s %-18s %-18s %10s %10s\n",
				truncateRunes(string(m.connectionLastAction[id]), 6), truncateRunes(tuiConnectionTarget(value), 26), truncateRunes(value.Network, 6),
				truncateRunes(tuiConnectionRule(value), 18), truncateRunes(emptyTUIDash(value.FinalNode), 18), tuiFormatBytes(value.Upload), tuiFormatBytes(value.Download)))
		}
	} else {
		for _, id := range ids[start:end] {
			value := m.connections[id]
			body.WriteString(fmt.Sprintf("%s  %s  %s\n", m.connectionLastAction[id], tuiConnectionTarget(value), emptyTUIDash(value.Network)))
			body.WriteString(fmt.Sprintf("  %s | %s | %s/%s\n", tuiConnectionRule(value), emptyTUIDash(strings.Join(value.Chains, " -> ")), tuiFormatBytes(value.Upload), tuiFormatBytes(value.Download)))
		}
	}
	if len(ids) == 0 {
		body.WriteString("(暂无活动连接)\n")
	}
	if len(m.connectionClosed) > 0 {
		body.WriteString(fmt.Sprintf("\n最近关闭：%s", tuiConnectionTarget(m.connectionClosed[0])))
		if len(m.connectionClosed) > 1 {
			body.WriteString(fmt.Sprintf(" 等 %d 条", len(m.connectionClosed)))
		}
		body.WriteString("\n")
	}
	state := "实时刷新"
	if m.connectionErr != nil {
		state = "读取失败：" + m.connectionErr.Error()
	} else if m.connectionFinished {
		state = "连接流已结束"
	}
	footer := fitFooter(fmt.Sprintf("↑/↓ 滚动  Esc 返回  Ctrl+C 退出 | 活动 %d | %s", len(ids), state), m.width)
	return fmt.Sprintf("%s\n\n实时连接\n%s\n%s", title, body.String(), footer)
}

func tuiConnectionTarget(value app.Connection) string {
	host := strings.TrimSpace(value.Host)
	if host == "" {
		host = strings.TrimSpace(value.DestinationIP)
	}
	if host == "" {
		host = "-"
	}
	if value.DestinationPort <= 0 {
		return host
	}
	return net.JoinHostPort(host, strconv.Itoa(value.DestinationPort))
}

func tuiConnectionRule(value app.Connection) string {
	rule := strings.TrimSpace(value.Rule)
	if value.RulePayload != "" {
		rule += ":" + value.RulePayload
	}
	return emptyTUIDash(rule)
}

func tuiFormatBytes(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%dB", value)
	}
	if value < 1024*1024 {
		return fmt.Sprintf("%.1fK", float64(value)/1024)
	}
	if value < 1024*1024*1024 {
		return fmt.Sprintf("%.1fM", float64(value)/(1024*1024))
	}
	return fmt.Sprintf("%.1fG", float64(value)/(1024*1024*1024))
}

func truncateRunes(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width <= 1 {
		return string(runes[:max(0, width)])
	}
	return string(runes[:width-1]) + "…"
}

func emptyTUIDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
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

func (m Model) renderModeView(title string) string {
	var body strings.Builder
	body.WriteString("运行模式\n")
	if m.modeStatus.ConfigMode == "" {
		if m.busy {
			body.WriteString("正在读取 daemon 状态...\n")
		} else if m.modeErr != nil {
			body.WriteString(modeErrorMessage(m.modeErr) + "\n")
		}
	} else {
		body.WriteString(fmt.Sprintf("配置模式：%s\n", routingModeLabel(m.modeStatus.ConfigMode)))
		if m.modeStatus.RuntimeAvailable && m.modeStatus.RuntimeMode != nil {
			body.WriteString(fmt.Sprintf("运行模式：%s\n", routingModeLabel(*m.modeStatus.RuntimeMode)))
		} else {
			body.WriteString(fmt.Sprintf("运行模式：不可用 / Core %s\n", coreStateLabel(m.modeStatus.CoreState)))
		}
		body.WriteString("实际路径：" + effectivePath(m.modeStatus) + "\n")
		if m.modeStatus.ConnectionsAvailable {
			body.WriteString(fmt.Sprintf("活动连接：%d\n", m.modeStatus.ActiveConnections))
		} else {
			body.WriteString("活动连接：不可用\n")
		}
		if m.modeStatus.ConnectionsClosed {
			body.WriteString("连接处理：已关闭现有连接\n")
		}
		body.WriteString("规则集：" + ruleSetSummary(m.modeStatus) + "\n")
		body.WriteString("监听入口：" + modeSummary(listenerSummary(m.modeStatus.Listeners), m.width) + "\n")
		body.WriteString("系统代理：" + modeSummary(proxySourceSummary(m.modeStatus.SystemProxy), m.width) + "\n")
		body.WriteString("CLI 环境代理：" + modeSummary(proxySourceSummary(m.modeStatus.EnvironmentProxy), m.width) + "\n")
		if m.modeStatus.NextStart {
			body.WriteString("生效状态：配置已保存，下次启动生效\n")
		}
	}
	body.WriteString("\n")
	modes := []domain.RoutingMode{domain.RoutingModeGlobal, domain.RoutingModeRule, domain.RoutingModeDirect}
	labels := []string{"全局代理", "规则分流", "全局直连"}
	for i, mode := range modes {
		cursor := "  "
		if m.actionIndex == i {
			cursor = "> "
		}
		radio := "○"
		if m.modeSelected == mode {
			radio = "●"
		}
		body.WriteString(fmt.Sprintf("%s%s %s\n", cursor, radio, labels[i]))
	}
	check := "[ ]"
	if m.modeClose {
		check = "[x]"
	}
	for index, label := range []string{check + " 关闭现有连接", "应用切换", "返回"} {
		cursor := "  "
		if m.actionIndex == index+3 {
			cursor = "> "
		}
		body.WriteString(cursor + label + "\n")
	}
	if m.busy {
		body.WriteString("\n正在处理，可按 Esc 取消\n")
	}
	if m.modeResult != "" {
		body.WriteString("\n" + m.modeResult + "\n")
	}
	if m.modeErr != nil && m.modeResult == "" && m.modeStatus.ConfigMode != "" {
		body.WriteString("\n" + modeErrorMessage(m.modeErr) + "\n")
	}
	for _, warning := range m.modeStatus.Warnings {
		body.WriteString("警告：" + warning + "\n")
	}
	footer := fitFooter("↑/↓ 选择  Enter 确认  Space 勾选  Esc 返回  q 退出", m.width)
	return fmt.Sprintf("%s\n\n%s\n%s", title, body.String(), footer)
}

func listenerSummary(values []app.ProxyListener) string {
	if len(values) == 0 {
		return "无"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, value.Protocol+" "+net.JoinHostPort(value.Host, strconv.Itoa(value.Port)))
	}
	return strings.Join(parts, "，")
}

func listenerPortActionItems(values []app.ListenerPort) []string {
	result := make([]string, 0, len(values)+1)
	for _, value := range values {
		result = append(result, value.Field)
	}
	return append(result, "返回")
}

func (m Model) renderListenerPortsView(title string) string {
	var body strings.Builder
	body.WriteString("配置管理 / 监听端口\n")
	if m.busy && m.listenerPortStatus.ProfileID == "" {
		body.WriteString("正在读取 daemon 配置...\n")
	} else {
		body.WriteString(fmt.Sprintf("档案：%s  Core：%s\n\n", m.listenerPortStatus.ProfileID, coreStateLabel(m.listenerPortStatus.CoreState)))
		for index, item := range m.listenerPortStatus.Ports {
			cursor := "  "
			if m.actionIndex == index {
				cursor = "> "
			}
			value := strconv.Itoa(item.Port)
			if !item.Enabled {
				value = "禁用"
			}
			conflict := ""
			for _, current := range m.listenerPortStatus.PortConflicts {
				if current.Field == item.Field {
					conflict = " [冲突]"
					break
				}
			}
			body.WriteString(fmt.Sprintf("%s%s  %s  %s %s%s\n", cursor, item.Field, value, strings.Join(item.Networks, "/"), item.Host, conflict))
		}
		cursor := "  "
		if m.actionIndex == len(m.listenerPortStatus.Ports) {
			cursor = "> "
		}
		body.WriteString(cursor + "返回\n")
	}
	if m.busy {
		body.WriteString("\n正在处理...\n")
	}
	if m.listenerPortResult != "" {
		body.WriteString("\n" + m.listenerPortResult + "\n")
	}
	footer := fitFooter("↑/↓ 选择  Enter 修改  Esc 返回  q 退出", m.width)
	return fmt.Sprintf("%s\n\n%s\n%s", title, body.String(), footer)
}

func (m Model) renderRuleSetView(title string) string {
	var body strings.Builder
	body.WriteString("配置管理 / CN 规则集\n")
	if m.rulesetStatus.Target.Ref != "" {
		body.WriteString(fmt.Sprintf("状态：%s\n固定引用：%s\n来源：%s\n", m.rulesetStatus.State, m.rulesetStatus.Target.Ref, m.rulesetStatus.Target.Source))
		body.WriteString(fmt.Sprintf("Domain：%s [%t]\nIP：%s [%t]\n", m.rulesetStatus.Domain.Path, m.rulesetStatus.Domain.DigestValid && m.rulesetStatus.Domain.FormatValid, m.rulesetStatus.IP.Path, m.rulesetStatus.IP.DigestValid && m.rulesetStatus.IP.FormatValid))
	} else if m.busy {
		body.WriteString("正在读取规则集状态...\n")
	} else {
		body.WriteString("规则集状态不可用\n")
	}
	if m.rulesetPhase != "" {
		body.WriteString("阶段：" + m.rulesetPhase + "\n")
	}
	if m.rulesetErr != nil {
		body.WriteString("错误：" + m.rulesetErr.Error() + "\n")
	}
	for index, item := range m.actionItems {
		cursor := "  "
		if index == m.actionIndex {
			cursor = "> "
		}
		body.WriteString(cursor + item + "\n")
	}
	if m.busy {
		body.WriteString("\n下载与校验中，可按 Esc 取消\n")
	}
	return fmt.Sprintf("%s\n\n%s\n%s", title, body.String(), fitFooter("↑/↓ 选择  Enter 确认  Esc 返回  q 退出", m.width))
}

func (m Model) renderProxyView(title string) string {
	var body strings.Builder
	name := "系统代理"
	if m.proxyLayer == "env" {
		name = "Bash 环境代理"
	}
	body.WriteString("配置管理 / " + name + "\n")
	if m.actionCtx == "proxy_confirm" {
		if m.proxyTarget == "restore" {
			body.WriteString("即将恢复启用前保存的整层设置。\n")
		} else if m.proxyTarget == "disable" {
			body.WriteString("即将删除 ~/.bashrc 中 mihomo-manager 管理区块。\n")
		} else {
			if m.proxyHost == "" {
				body.WriteString(fmt.Sprintf("即将按当前 mihomo listener 设置 %s。\n", m.proxyTarget))
			} else {
				body.WriteString(fmt.Sprintf("即将设置 %s 为 %s:%d。\n", m.proxyTarget, m.proxyHost, m.proxyPort))
			}
		}
		for index, item := range m.actionItems {
			cursor := "  "
			if index == m.actionIndex {
				cursor = "> "
			}
			body.WriteString(cursor + item + "\n")
		}
		return fmt.Sprintf("%s\n\n%s\n%s", title, body.String(), fitFooter("↑/↓ 选择  Enter 确认  Esc 取消  q 退出", m.width))
	}
	body.WriteString(fmt.Sprintf("档案：%s  Core：%s  manager 管理：%t  快照：%t\n", m.proxyStatus.ProfileID, coreStateLabel(m.proxyStatus.CoreState), m.proxyStatus.Managed, m.proxyStatus.SnapshotAvailable))
	for _, item := range m.proxyStatus.Endpoints {
		endpoint := "禁用"
		if item.Host != "" && item.Port > 0 {
			endpoint = item.Scheme + "://" + net.JoinHostPort(item.Host, strconv.Itoa(item.Port))
		}
		body.WriteString(fmt.Sprintf("%s: %s [%s]\n", item.Target, endpoint, item.State))
	}
	if m.busy {
		body.WriteString("正在处理...\n")
	}
	for index, item := range m.actionItems {
		cursor := "  "
		if index == m.actionIndex {
			cursor = "> "
		}
		body.WriteString(cursor + item + "\n")
	}
	return fmt.Sprintf("%s\n\n%s\n%s", title, body.String(), fitFooter("↑/↓ 选择  Enter 执行  Esc 返回  q 退出", m.width))
}

func (m Model) renderDaemonRestartConfirm(title string) string {
	width := 76
	if m.width > 0 {
		width = m.width - 4
	}
	if width < 20 {
		width = 20
	}
	warning := wrapDisplayText("重启全部会先停止 Core，再重启 mihomo-manager daemon，并按当前状态恢复 Core。代理连接会短暂中断，测速历史、实时连接等 Core 内存状态会丢失。", width)
	footer := fitFooter("Enter 确认重启  Esc 取消", m.width)
	return fmt.Sprintf("%s\n\n服务管理 / 重启全部\n%s\n\n%s", title, warning, footer)
}

func (m Model) renderDaemonRestartProgress(title string) string {
	phases := []app.DaemonRestartPhase{
		app.DaemonRestartPhasePreflight,
		app.DaemonRestartPhaseStopCore,
		app.DaemonRestartPhaseRestartDaemon,
		app.DaemonRestartPhaseWaitDaemon,
		app.DaemonRestartPhaseRestoreCore,
		app.DaemonRestartPhaseVerify,
	}
	if m.daemonRestartProgress.Phase == app.DaemonRestartPhaseRecover {
		phases = append(phases, app.DaemonRestartPhaseRecover)
	}
	var body strings.Builder
	for _, phase := range phases {
		marker := "[ ]"
		step := daemonRestartPhaseStep(phase)
		switch {
		case phase == m.daemonRestartProgress.Phase:
			marker = "[>]"
		case step < m.daemonRestartProgress.Step && m.daemonRestartSeen[phase]:
			marker = "[x]"
		case step < m.daemonRestartProgress.Step:
			marker = "[-]"
		}
		body.WriteString(fmt.Sprintf("%s %s\n", marker, daemonRestartPhaseLabel(phase)))
	}
	width := 76
	if m.width > 0 {
		width = max(20, m.width-4)
	}
	message := wrapDisplayText(m.daemonRestartProgress.Message, width)
	footer := fitFooter("操作进行中，不可取消", m.width)
	return fmt.Sprintf("%s\n\n服务管理 / 重启全部\n%s\n%s\n\n%s", title, body.String(), message, footer)
}

func formatDaemonRestartResult(result app.DaemonRestartResult, err error) string {
	var body strings.Builder
	switch {
	case err == nil:
		body.WriteString("重启全部完成\n")
	case result.RecoverySucceeded:
		body.WriteString("组合重启异常，服务已恢复\n")
	default:
		body.WriteString("组合重启失败，服务状态需要复核\n")
	}
	body.WriteString(fmt.Sprintf("daemon PID: %d -> %d\n", result.PreviousDaemonPID, result.DaemonPID))
	body.WriteString(fmt.Sprintf("Core 状态: %s -> %s\n", result.PreviousCoreState, result.CoreState))
	body.WriteString("daemon 已更换: " + tuiBoolLabel(result.DaemonRestarted) + "\n")
	body.WriteString("Core 已恢复: " + tuiCoreRestoreLabel(result) + "\n")
	if result.RecoveryAttempted {
		body.WriteString("自动恢复: ")
		if result.RecoverySucceeded {
			body.WriteString("成功\n")
		} else {
			body.WriteString("失败\n")
		}
	}
	if result.FailurePhase != "" {
		body.WriteString("失败阶段: " + daemonRestartPhaseLabel(result.FailurePhase) + "\n")
	}
	return strings.TrimSpace(body.String())
}

func daemonRestartPhaseStep(phase app.DaemonRestartPhase) int {
	switch phase {
	case app.DaemonRestartPhasePreflight:
		return 1
	case app.DaemonRestartPhaseStopCore:
		return 2
	case app.DaemonRestartPhaseRestartDaemon:
		return 3
	case app.DaemonRestartPhaseWaitDaemon:
		return 4
	case app.DaemonRestartPhaseRestoreCore:
		return 5
	case app.DaemonRestartPhaseVerify:
		return 6
	case app.DaemonRestartPhaseRecover:
		return 7
	default:
		return 0
	}
}

func daemonRestartPhaseLabel(phase app.DaemonRestartPhase) string {
	switch phase {
	case app.DaemonRestartPhasePreflight:
		return "预检"
	case app.DaemonRestartPhaseStopCore:
		return "停止 Core"
	case app.DaemonRestartPhaseRestartDaemon:
		return "重启 daemon"
	case app.DaemonRestartPhaseWaitDaemon:
		return "等待握手"
	case app.DaemonRestartPhaseRestoreCore:
		return "恢复 Core"
	case app.DaemonRestartPhaseVerify:
		return "最终验证"
	case app.DaemonRestartPhaseRecover:
		return "自动恢复"
	default:
		return string(phase)
	}
}

func tuiBoolLabel(value bool) string {
	if value {
		return "是"
	}
	return "否"
}

func tuiCoreRestoreLabel(result app.DaemonRestartResult) string {
	if result.PreviousCoreState == domain.CoreStateStopped {
		return "无需"
	}
	return tuiBoolLabel(result.CoreRestored)
}

func wrapDisplayText(value string, width int) string {
	if width <= 0 || lipgloss.Width(value) <= width {
		return value
	}
	var lines []string
	var line strings.Builder
	for _, character := range value {
		candidate := line.String() + string(character)
		if line.Len() > 0 && lipgloss.Width(candidate) > width {
			lines = append(lines, line.String())
			line.Reset()
		}
		line.WriteRune(character)
	}
	if line.Len() > 0 {
		lines = append(lines, line.String())
	}
	return strings.Join(lines, "\n")
}

func proxySourceSummary(values []app.ProxySourceStatus) string {
	if len(values) == 0 {
		return "未检测"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, value.Source+"="+value.State)
	}
	return strings.Join(parts, "，")
}

func modeSummary(value string, width int) string {
	if width <= 0 {
		return value
	}
	return truncateRunes(value, max(20, width-12))
}

func routingModeLabel(mode domain.RoutingMode) string {
	switch mode {
	case domain.RoutingModeGlobal:
		return "全局代理"
	case domain.RoutingModeRule:
		return "规则分流"
	case domain.RoutingModeDirect:
		return "全局直连"
	default:
		return mode.String()
	}
}

func effectivePath(status app.RoutingModeStatus) string {
	node := strings.TrimSpace(status.EffectiveNode)
	if node == "" {
		node = "当前节点待 Core 启动后确认"
	}
	switch status.ConfigMode {
	case domain.RoutingModeGlobal:
		return "全部流量 -> GLOBAL -> " + node
	case domain.RoutingModeDirect:
		return "全部流量 -> DIRECT"
	default:
		return "非大陆流量 -> 🌐 代理 -> " + node
	}
}

func ruleSetSummary(status app.RoutingModeStatus) string {
	if status.ConfigMode != domain.RoutingModeRule {
		return "当前模式无需核验"
	}
	if !status.RuntimeAvailable {
		return "待 Core 启动后核验"
	}
	parts := make([]string, 0, len(status.RuleSets))
	for _, item := range status.RuleSets {
		state := "异常"
		if item.Available && item.Loaded {
			state = "正常"
		}
		parts = append(parts, item.Name+" "+state)
	}
	if len(parts) == 0 {
		return "未报告"
	}
	return strings.Join(parts, "，")
}
