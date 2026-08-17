// Package discord provides player-facing Discord commands and state-change notices.
package discord

import (
	"context"
	"fmt"
	"strings"

	"github.com/alexandergg-0520/voxellink-monitor/internal/domain"
	"github.com/bwmarrin/discordgo"
)

type ChannelRepository interface {
	NotificationChannelIDs(context.Context, string) ([]string, error)
}
type QueryRepository interface {
	SnapshotByExternalID(context.Context, string) (domain.ServerSnapshot, error)
	Uptime24h(context.Context, string) (float64, error)
	RecentIncidents(context.Context, string, int) ([]domain.Incident, error)
}

type Notifier struct {
	session  *discordgo.Session
	channels ChannelRepository
}

func NewNotifier(token string, channels ChannelRepository) (*Notifier, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}
	return &Notifier{session: session, channels: channels}, nil
}
func (n *Notifier) StateChanged(ctx context.Context, server domain.Server, state domain.PublicStatus, result domain.CheckResult) error {
	channelIDs, err := n.channels.NotificationChannelIDs(ctx, server.ID)
	if err != nil {
		return err
	}
	for _, channelID := range channelIDs {
		if _, err := n.session.ChannelMessageSend(channelID, statusMessage(server, state, result)); err != nil {
			return fmt.Errorf("send channel %s: %w", channelID, err)
		}
	}
	return nil
}

type Bot struct {
	session *discordgo.Session
	queries QueryRepository
}

func NewBot(token string, queries QueryRepository) (*Bot, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}
	return &Bot{session: session, queries: queries}, nil
}
func (b *Bot) Run(ctx context.Context) error {
	b.session.AddHandler(b.handleInteraction)
	if err := b.session.Open(); err != nil {
		return fmt.Errorf("open Discord gateway: %w", err)
	}
	defer b.session.Close()
	applicationID := b.session.State.User.ID
	if _, err := b.session.ApplicationCommandBulkOverwrite(applicationID, "", commands()); err != nil {
		return fmt.Errorf("register application commands: %w", err)
	}
	<-ctx.Done()
	return ctx.Err()
}
func commands() []*discordgo.ApplicationCommand {
	serverOption := &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionString, Name: "server", Description: "VoxelLink server ID", Required: true}
	return []*discordgo.ApplicationCommand{
		{Name: "status", Description: "Show the current player-facing server status", Options: []*discordgo.ApplicationCommandOption{serverOption}},
		{Name: "uptime", Description: "Show availability over the last 24 hours", Options: []*discordgo.ApplicationCommandOption{serverOption}},
		{Name: "incidents", Description: "Show recent incidents", Options: []*discordgo.ApplicationCommandOption{serverOption}},
	}
}
func (b *Bot) handleInteraction(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
	if interaction.Type != discordgo.InteractionApplicationCommand {
		return
	}
	data := interaction.ApplicationCommandData()
	serverID := data.Options[0].StringValue()
	snapshot, err := b.queries.SnapshotByExternalID(context.Background(), serverID)
	if err != nil {
		respond(session, interaction, "そのVoxelLink掲載サーバーは見つかりません。", true)
		return
	}
	var content string
	switch data.Name {
	case "status":
		content = snapshotMessage(snapshot)
	case "uptime":
		uptime, err := b.queries.Uptime24h(context.Background(), snapshot.ID)
		if err != nil {
			respond(session, interaction, "稼働率を取得できませんでした。", true)
			return
		}
		content = fmt.Sprintf("**%s**\n直近24時間の稼働率: **%.2f%%**", snapshot.Name, uptime)
	case "incidents":
		incidents, err := b.queries.RecentIncidents(context.Background(), snapshot.ID, 5)
		if err != nil {
			respond(session, interaction, "Incident履歴を取得できませんでした。", true)
			return
		}
		content = incidentsMessage(snapshot.Name, incidents)
	default:
		return
	}
	respond(session, interaction, content, false)
}
func respond(session *discordgo.Session, interaction *discordgo.InteractionCreate, content string, ephemeral bool) {
	flags := discordgo.MessageFlags(0)
	if ephemeral {
		flags = discordgo.MessageFlagsEphemeral
	}
	_ = session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Content: content, Flags: flags}})
}
func statusMessage(server domain.Server, state domain.PublicStatus, result domain.CheckResult) string {
	switch state {
	case domain.Outage:
		return fmt.Sprintf("🔴 **%s** に現在接続できません\nVoxelLink Monitorが正常なMinecraft応答を確認できていません。", server.Name)
	case domain.Operational:
		return fmt.Sprintf("🟢 **%s** が復旧しました\n現在は正常に接続できます。", server.Name)
	case domain.Unknown:
		return fmt.Sprintf("⚪ **%s** の状態を確認できません\nMonitor側の問題の可能性があり、サーバー障害とは判定していません。", server.Name)
	case domain.Maintenance:
		return fmt.Sprintf("🔵 **%s** はメンテナンス中です。", server.Name)
	default:
		return fmt.Sprintf("🟡 **%s** は正常応答していますが、状態を確認してください。", server.Name)
	}
}
func snapshotMessage(snapshot domain.ServerSnapshot) string {
	icon := map[domain.PublicStatus]string{domain.Operational: "🟢", domain.Degraded: "🟡", domain.Outage: "🔴", domain.Maintenance: "🔵", domain.Unknown: "⚪"}[snapshot.Status]
	parts := []string{fmt.Sprintf("%s **%s**", icon, snapshot.Name), string(snapshot.Status)}
	if snapshot.LastOutcome != "" {
		details := fmt.Sprintf("最終確認: %s", snapshot.LastCheckedAt.Format("2006-01-02 15:04 MST"))
		if snapshot.Latency > 0 {
			details += fmt.Sprintf(" / %d ms", snapshot.Latency.Milliseconds())
		}
		parts = append(parts, details)
	}
	return strings.Join(parts, "\n")
}
func incidentsMessage(name string, incidents []domain.Incident) string {
	if len(incidents) == 0 {
		return fmt.Sprintf("**%s**\n最近のIncidentはありません。", name)
	}
	lines := []string{fmt.Sprintf("**%s** の最近のIncident", name)}
	for _, incident := range incidents {
		label := "継続中"
		if incident.ResolvedAt != nil {
			label = "解決済み"
		}
		lines = append(lines, fmt.Sprintf("- %s — %s（%s）", incident.StartedAt.Format("2006-01-02 15:04 MST"), label, incident.Reason))
	}
	return strings.Join(lines, "\n")
}
