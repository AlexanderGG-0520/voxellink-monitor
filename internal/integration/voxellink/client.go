// Package voxellink contains the outbound boundary to VoxelLink. No monitor
// loop calls it: it is used only for explicit imports and periodic metadata sync.
package voxellink

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alexandergg-0520/voxellink-monitor/internal/domain"
)

type Client struct {
	baseURL, token string
	httpClient     *http.Client
}

func New(baseURL, token string) (*Client, error) {
	if baseURL == "" || token == "" {
		return nil, fmt.Errorf("VOXELLINK_API_BASE_URL and VOXELLINK_API_TOKEN must be set")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid VoxelLink API base URL")
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, httpClient: &http.Client{Timeout: 10 * time.Second}}, nil
}

// FetchServer expects the Monitor integration contract documented in
// docs/voxellink-integration.md. VoxelLink alone decides whether the supplied
// service credential may read the listing and its verified members.
func (c *Client) FetchServer(ctx context.Context, externalID string) (domain.ImportedServer, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/monitor/servers/"+url.PathEscape(externalID), nil)
	if err != nil {
		return domain.ImportedServer{}, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return domain.ImportedServer{}, fmt.Errorf("request VoxelLink: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return domain.ImportedServer{}, fmt.Errorf("VoxelLink returned %s", response.Status)
	}
	var payload struct {
		Server struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Hostname  string `json:"hostname"`
			Port      int    `json:"port"`
			Transport string `json:"transport"`
		} `json:"server"`
		Members []struct {
			DiscordUserID string `json:"discord_user_id"`
			Role          string `json:"role"`
		} `json:"members"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return domain.ImportedServer{}, fmt.Errorf("decode VoxelLink response: %w", err)
	}
	if payload.Server.ID == "" || payload.Server.Name == "" || payload.Server.Hostname == "" || payload.Server.Port < 1 || payload.Server.Port > 65535 {
		return domain.ImportedServer{}, fmt.Errorf("VoxelLink returned invalid monitor server data")
	}
	transport := payload.Server.Transport
	if transport == "" {
		transport = "DIRECT"
	}
	if transport != "DIRECT" && transport != "CLOUDFLARE_SPECTRUM" && transport != "CLOUDFLARE_TUNNEL" {
		return domain.ImportedServer{}, fmt.Errorf("unsupported transport %q", transport)
	}
	server := domain.ImportedServer{ExternalID: payload.Server.ID, Name: payload.Server.Name, Hostname: payload.Server.Hostname, Port: payload.Server.Port, Transport: transport}
	for _, member := range payload.Members {
		if member.DiscordUserID != "" && (member.Role == "owner" || member.Role == "manager" || member.Role == "viewer") {
			server.Members = append(server.Members, domain.ServerMember{DiscordUserID: member.DiscordUserID, Role: member.Role})
		}
	}
	return server, nil
}
