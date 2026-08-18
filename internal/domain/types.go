package domain

import "time"

type PublicStatus string

const (
	Operational PublicStatus = "OPERATIONAL"
	Degraded    PublicStatus = "DEGRADED"
	Outage      PublicStatus = "OUTAGE"
	Maintenance PublicStatus = "MAINTENANCE"
	Unknown     PublicStatus = "UNKNOWN"
)

type Outcome string

const (
	Success               Outcome = "SUCCESS"
	DNSFailure            Outcome = "DNS_FAILURE"
	ConnectionRefused     Outcome = "CONNECTION_REFUSED"
	ConnectTimeout        Outcome = "CONNECT_TIMEOUT"
	StatusTimeout         Outcome = "STATUS_TIMEOUT"
	ConnectionReset       Outcome = "CONNECTION_RESET"
	InvalidStatusResponse Outcome = "INVALID_STATUS_RESPONSE"
	ProbeError            Outcome = "PROBE_ERROR"
)

type CheckResult struct {
	Outcome                   Outcome
	Latency                   time.Duration
	PlayersOnline, PlayersMax int
	Detail                    string
	At                        time.Time
}
type Server struct {
	ID, Name, Hostname string
	Port               int
	Status             PublicStatus
	Transport          string
	Enabled            bool
}

// ImportedServer is the local immutable copy of a VoxelLink listing needed to
// continue monitoring while VoxelLink is unavailable.
type ImportedServer struct {
	ExternalID string
	Name       string
	Hostname   string
	Port       int
	Transport  string
	Members    []ServerMember
}
type ServerMember struct{ DiscordUserID, Role string }

type ServerSnapshot struct {
	Server
	LastCheckedAt time.Time
	LastOutcome   Outcome
	Latency       time.Duration
}

type Incident struct {
	ID, ServerID           string
	State                  string
	Reason                 Outcome
	StartedAt, ConfirmedAt time.Time
	ResolvedAt             *time.Time
}

type UserReportType string

const (
	ReportConnection UserReportType = "CONNECTION"
	ReportLogin      UserReportType = "LOGIN"
	ReportTimeout    UserReportType = "TIMEOUT"
	ReportLag        UserReportType = "LAG"
	ReportOther      UserReportType = "OTHER"
)

type UserReport struct {
	ServerID, ReporterHash, Detail string
	Type                           UserReportType
	At                             time.Time
}

// CrowdSignal is the passive-report evidence currently affecting a server.
// It never overrides a confirmed active-probe OUTAGE.
type CrowdSignal struct {
	Reports, Baseline, Threshold int
	Anomalous                    bool
}

type PendingStateNotification struct {
	ID     int64
	Server Server
	State  PublicStatus
	Result CheckResult
}

type RetentionStats struct{ RawDeleted, FifteenMinuteDeleted, HourlyDeleted int64 }
