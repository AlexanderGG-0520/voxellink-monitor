package integration

import (
	"context"
	"github.com/alexandergg-0520/voxellink-monitor/internal/domain"
	"testing"
)

type syncProvider struct{ fetched []string }

func (p *syncProvider) FetchServer(_ context.Context, id string) (domain.ImportedServer, error) {
	p.fetched = append(p.fetched, id)
	return domain.ImportedServer{ExternalID: id, Name: id, Hostname: "example.test", Port: 25565, Transport: "DIRECT"}, nil
}

type syncRepository struct{ ids, stored []string }

func (r *syncRepository) VoxelLinkExternalServerIDs(context.Context) ([]string, error) {
	return r.ids, nil
}
func (r *syncRepository) UpsertVoxelLinkServer(_ context.Context, server domain.ImportedServer) (domain.Server, error) {
	r.stored = append(r.stored, server.ExternalID)
	return domain.Server{ID: server.ExternalID}, nil
}
func TestSynchronizerRefreshesEveryImportedServer(t *testing.T) {
	provider := &syncProvider{}
	repository := &syncRepository{ids: []string{"a", "b"}}
	syncer := NewSynchronizer(NewImporter(provider, repository), repository)
	if err := syncer.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(provider.fetched) != 2 || len(repository.stored) != 2 {
		t.Fatalf("fetched=%v stored=%v", provider.fetched, repository.stored)
	}
}
