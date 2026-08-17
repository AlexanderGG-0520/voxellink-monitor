package domain

import "time"

type PublicStatus string
const (
	Operational PublicStatus = "OPERATIONAL"; Degraded PublicStatus = "DEGRADED"; Outage PublicStatus = "OUTAGE"; Maintenance PublicStatus = "MAINTENANCE"; Unknown PublicStatus = "UNKNOWN"
)
type Outcome string
const (
	Success Outcome = "SUCCESS"; DNSFailure Outcome = "DNS_FAILURE"; ConnectionRefused Outcome = "CONNECTION_REFUSED"; ConnectTimeout Outcome = "CONNECT_TIMEOUT"; StatusTimeout Outcome = "STATUS_TIMEOUT"; ConnectionReset Outcome = "CONNECTION_RESET"; InvalidStatusResponse Outcome = "INVALID_STATUS_RESPONSE"; ProbeError Outcome = "PROBE_ERROR"
)
type CheckResult struct { Outcome Outcome; Latency time.Duration; PlayersOnline, PlayersMax int; Detail string; At time.Time }
type Server struct { ID, Name, Hostname string; Port int; Status PublicStatus; Transport string; Enabled bool }

