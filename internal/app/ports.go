package app

import (
	"context"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

type CoreStatus struct {
	Type          domain.CoreType
	State         domain.CoreState
	ProfileID     domain.ProfileID
	PID           int
	PortConflicts []PortConflict
}

type Profile struct {
	ID       domain.ProfileID
	Name     string
	Mode     string
	CoreType domain.CoreType
	Active   bool
}

type Node struct {
	ID             domain.NodeID
	SubscriptionID domain.SubscriptionID
	Name           string
	Protocol       string
}

type Group struct {
	ID             domain.GroupID
	Name           string
	Type           string
	SelectedNodeID domain.NodeID
	NodeIDs        []domain.NodeID
}

type Subscription struct {
	ID            domain.SubscriptionID
	Name          string
	URL           string
	Enabled       bool
	LastUpdatedAt time.Time
}

type NodeDelay struct {
	NodeID domain.NodeID
	Delay  time.Duration
}

type NodeTestRequest struct {
	ProfileID   domain.ProfileID
	GroupID     domain.GroupID
	Concurrency int
	Limit       int
}

type NodeTestEvent struct {
	Done     int
	Total    int
	Result   *NodeDelay
	Err      error
	Finished bool
}

type RouteDiagnosis struct {
	Input       string
	Host        string
	MatchedRule string
	Target      string
	CurrentNode string
	Confidence  string
	Note        string
}

type Connection struct {
	ID              string
	Host            string
	DestinationIP   string
	DestinationPort int
	Network         string
	Rule            string
	RulePayload     string
	Chains          []string
	FinalNode       string
	Upload          int64
	Download        int64
	Start           time.Time
	Warnings        []string
}

type ConnectionRequest struct {
	ProfileID domain.ProfileID
}

type ConnectionAction string

const (
	ConnectionActionOpen   ConnectionAction = "open"
	ConnectionActionUpdate ConnectionAction = "update"
	ConnectionActionClosed ConnectionAction = "closed"
)

type ConnectionEvent struct {
	Seq        uint64
	Action     ConnectionAction
	Connection *Connection
	Time       time.Time
	Err        error
	Finished   bool
}

type RuleSetHealth struct {
	Name      string
	Available bool
	Loaded    bool
}

type RoutingModeStatus struct {
	ProfileID            domain.ProfileID
	ConfigMode           domain.RoutingMode
	RuntimeMode          *domain.RoutingMode
	RuntimeAvailable     bool
	CoreState            domain.CoreState
	EffectiveGroup       string
	EffectiveNode        string
	RuleSets             []RuleSetHealth
	ActiveConnections    int
	ConnectionsAvailable bool
	ConnectionsClosed    bool
	NextStart            bool
	OperationID          domain.OperationID
	OperationPhase       string
	Warnings             []string
	Listeners            []ProxyListener
	SystemProxy          []ProxySourceStatus
	EnvironmentProxy     []ProxySourceStatus
}

type ProxyListener struct {
	Protocol string
	Host     string
	Port     int
}

type ProxyEndpoint struct {
	Scheme string
	Host   string
	Port   int
}

type ProxySourceStatus struct {
	Source           string
	ExpectedProtocol string
	State            string
	Endpoint         *ProxyEndpoint
	Warning          string
}

type SetRoutingModeRequest struct {
	ProfileID        domain.ProfileID
	Mode             domain.RoutingMode
	CloseConnections bool
	RequestID        string
}

type RenderedConfig struct {
	Content []byte
}

type ConfigDiff struct {
	Content string
}

type LogRequest struct {
	ProfileID domain.ProfileID
	Lines     int
}

type LogLine struct {
	Time    time.Time
	Level   string
	Message string
}

type LogEvent struct {
	Line     *LogLine
	Err      error
	Finished bool
}

type CoreService interface {
	Status(context.Context, domain.ProfileID) (CoreStatus, error)
	Start(context.Context, domain.ProfileID) error
	Stop(context.Context, domain.ProfileID) error
	Restart(context.Context, domain.ProfileID) error
	Reload(context.Context, domain.ProfileID) error
}

type ProfileService interface {
	List(context.Context) ([]Profile, error)
	Show(context.Context, domain.ProfileID) (Profile, error)
	Use(context.Context, domain.ProfileID) error
}

type NodeService interface {
	List(context.Context, domain.ProfileID, domain.GroupID) ([]Node, error)
	Test(context.Context, NodeTestRequest) <-chan NodeTestEvent
}

type GroupService interface {
	List(context.Context, domain.ProfileID) ([]Group, error)
	Show(context.Context, domain.ProfileID, domain.GroupID) (Group, error)
	Select(context.Context, domain.ProfileID, domain.GroupID, domain.NodeID) error
}

type SubscriptionService interface {
	List(context.Context, domain.ProfileID) ([]Subscription, error)
	Show(context.Context, domain.ProfileID, domain.SubscriptionID) (Subscription, error)
	SetURL(context.Context, domain.ProfileID, domain.SubscriptionID, string) error
	Update(context.Context, domain.ProfileID, domain.SubscriptionID) error
}

type RouteService interface {
	Diagnose(context.Context, domain.ProfileID, string) (RouteDiagnosis, error)
	ListWhitelist(context.Context, domain.ProfileID) ([]string, error)
	AddWhitelist(context.Context, domain.ProfileID, string) error
	RemoveWhitelist(context.Context, domain.ProfileID, string) error
	ApplyPreset(context.Context, domain.ProfileID, string) error
}

type ConnectionService interface {
	Connections(context.Context, ConnectionRequest) ([]Connection, error)
	FollowConnections(context.Context, ConnectionRequest) <-chan ConnectionEvent
}

type ModeService interface {
	ModeStatus(context.Context, domain.ProfileID) (RoutingModeStatus, error)
	SetMode(context.Context, SetRoutingModeRequest) (RoutingModeStatus, error)
}

type ConfigService interface {
	Render(context.Context, domain.ProfileID) (RenderedConfig, error)
	Diff(context.Context, domain.ProfileID) (ConfigDiff, error)
	Validate(context.Context, domain.ProfileID) error
	Backup(context.Context, domain.ProfileID) error
	Restore(context.Context, domain.ProfileID) error
	Edit(context.Context, domain.ProfileID) error
}

type LogService interface {
	Tail(context.Context, LogRequest) ([]LogLine, error)
	Follow(context.Context, LogRequest) <-chan LogEvent
}
