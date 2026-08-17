package integration

import (
	"context"
	"fmt"
)

type ExternalIDRepository interface {
	VoxelLinkExternalServerIDs(context.Context) ([]string, error)
}
type Synchronizer struct {
	importer   *Importer
	repository ExternalIDRepository
}

func NewSynchronizer(importer *Importer, repository ExternalIDRepository) *Synchronizer {
	return &Synchronizer{importer: importer, repository: repository}
}

// Sync refreshes imported listing metadata but never participates in probing.
// A VoxelLink outage therefore leaves existing monitoring untouched.
func (s *Synchronizer) Sync(ctx context.Context) error {
	ids, err := s.repository.VoxelLinkExternalServerIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := s.importer.Import(ctx, id); err != nil {
			return fmt.Errorf("refresh %s: %w", id, err)
		}
	}
	return nil
}
