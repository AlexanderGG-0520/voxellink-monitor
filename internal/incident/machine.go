// Package incident turns individual probe observations into stable public state.
package incident

import "github.com/alexandergg-0520/voxellink-monitor/internal/domain"

type Machine struct { failures, successes int; state domain.PublicStatus }
func New(state domain.PublicStatus) *Machine { return &Machine{state: state} }
func (m *Machine) State() domain.PublicStatus { return m.state }
// Apply follows the v1 contract: three consecutive failures open an outage and
// two consecutive successes close it. Probe-side failures become UNKNOWN.
func (m *Machine) Apply(r domain.CheckResult) (domain.PublicStatus, bool) {
	if r.Outcome == domain.ProbeError { changed := m.state != domain.Unknown; m.state = domain.Unknown; m.failures=0; m.successes=0; return m.state, changed }
	if r.Outcome == domain.Success { m.failures=0; if m.state == domain.Outage { m.successes++; if m.successes < 2 { return m.state, false }; m.state=domain.Operational; m.successes=0; return m.state, true }; m.successes=0; if m.state != domain.Operational { m.state=domain.Operational; return m.state, true }; return m.state, false }
	m.successes=0; m.failures++; if m.failures >= 3 && m.state != domain.Outage { m.state=domain.Outage; return m.state, true }; return m.state, false
}
