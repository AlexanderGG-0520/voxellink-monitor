package voxellink

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization = %q", got)
		}
		if r.URL.Path != "/api/v1/monitor/servers/abc-123" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"server":{"id":"abc-123","name":"Example","hostname":"play.example.com","port":25565,"transport":"DIRECT"},"members":[{"discord_user_id":"42","role":"owner"}]}`))
	}))
	defer server.Close()
	client, err := New(server.URL, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.FetchServer(context.Background(), "abc-123")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Example" || len(got.Members) != 1 || got.Members[0].Role != "owner" {
		t.Fatalf("unexpected import: %#v", got)
	}
}
