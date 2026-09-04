package orphan

import (
	"context"
	"errors"

	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// storeClusterLookup adapts *store.ClusterStore to the ClusterLookup interface.
type storeClusterLookup struct {
	cs *store.ClusterStore
}

// NewStoreClusterLookup returns a ClusterLookup backed by the cluster store.
func NewStoreClusterLookup(cs *store.ClusterStore) ClusterLookup {
	return storeClusterLookup{cs: cs}
}

func (l storeClusterLookup) ClusterStatusByID(ctx context.Context, id string) (types.ClusterStatus, bool, error) {
	c, err := l.cs.GetByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return c.Status, true, nil
}

func (l storeClusterLookup) MostRecentClusterStatusByName(ctx context.Context, name string) (types.ClusterStatus, bool, error) {
	id, err := l.cs.GetMostRecentIDByName(ctx, name)
	if err != nil {
		return "", false, err
	}
	if id == "" {
		return "", false, nil
	}
	return l.ClusterStatusByID(ctx, id)
}
