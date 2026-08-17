package integration

import (
	"context"
	"fmt"
	"github.com/alexandergg-0520/voxellink-monitor/internal/domain"
)

type VoxelLinkProvider interface {
	FetchServer(context.Context, string) (domain.ImportedServer, error)
}
type ImportRepository interface {
	UpsertVoxelLinkServer(context.Context, domain.ImportedServer) (domain.Server, error)
}
type Importer struct {
	provider   VoxelLinkProvider
	repository ImportRepository
}

func NewImporter(provider VoxelLinkProvider, repository ImportRepository) *Importer {
	return &Importer{provider: provider, repository: repository}
}
func (i *Importer) Import(ctx context.Context, externalID string) (domain.Server, error) {
	if externalID == "" {
		return domain.Server{}, fmt.Errorf("external server ID is required")
	}
	imported, err := i.provider.FetchServer(ctx, externalID)
	if err != nil {
		return domain.Server{}, err
	}
	return i.repository.UpsertVoxelLinkServer(ctx, imported)
}
