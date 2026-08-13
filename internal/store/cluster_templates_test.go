package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/dbtest"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

func newTemplateOwner(t *testing.T, s *store.Store) *types.User {
	t.Helper()
	ctx := context.Background()
	id := uuid.New().String()
	user := &types.User{
		ID:           id,
		Email:        id + "@example.com",
		Username:     id,
		PasswordHash: "x",
		Role:         types.RoleUser,
		Timezone:     "UTC",
		WorkDays:     62,
		Active:       true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	require.NoError(t, s.Users.Create(ctx, user))
	return user
}

func newTemplate(ownerID, name string) *types.ClusterTemplate {
	return &types.ClusterTemplate{
		ID:        uuid.New().String(),
		Name:      name,
		OwnerID:   ownerID,
		Config:    json.RawMessage(`{"platform":"aws"}`),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

func TestClusterTemplateStore_Create_EnforcesPerUserLimit(t *testing.T) {
	s := dbtest.New(t)
	ctx := context.Background()
	owner := newTemplateOwner(t, s)

	for i := 0; i < store.MaxTemplatesPerUser; i++ {
		err := s.ClusterTemplates.Create(ctx, newTemplate(owner.ID, fmt.Sprintf("template-%d", i)))
		require.NoError(t, err, "creating template %d within the limit should succeed", i)
	}

	err := s.ClusterTemplates.Create(ctx, newTemplate(owner.ID, "one-too-many"))
	require.ErrorIs(t, err, store.ErrTemplateLimitReached)

	list, err := s.ClusterTemplates.List(ctx, owner.ID)
	require.NoError(t, err)
	require.Len(t, list, store.MaxTemplatesPerUser, "the rejected template must not have been inserted")
}

func TestClusterTemplateStore_Create_LimitIsPerOwner(t *testing.T) {
	s := dbtest.New(t)
	ctx := context.Background()
	owner1 := newTemplateOwner(t, s)
	owner2 := newTemplateOwner(t, s)

	for i := 0; i < store.MaxTemplatesPerUser; i++ {
		require.NoError(t, s.ClusterTemplates.Create(ctx, newTemplate(owner1.ID, fmt.Sprintf("t-%d", i))))
	}

	// A different owner at another owner's limit is unaffected.
	require.NoError(t, s.ClusterTemplates.Create(ctx, newTemplate(owner2.ID, "t-0")))
}

func TestClusterTemplateStore_ScopedToOwner(t *testing.T) {
	s := dbtest.New(t)
	ctx := context.Background()
	owner := newTemplateOwner(t, s)
	other := newTemplateOwner(t, s)

	tmpl := newTemplate(owner.ID, "mine")
	require.NoError(t, s.ClusterTemplates.Create(ctx, tmpl))

	// Another user cannot read, update, or delete it.
	_, err := s.ClusterTemplates.GetByID(ctx, tmpl.ID, other.ID)
	require.Error(t, err)

	require.Error(t, s.ClusterTemplates.Delete(ctx, tmpl.ID, other.ID))

	// The owner can.
	got, err := s.ClusterTemplates.GetByID(ctx, tmpl.ID, owner.ID)
	require.NoError(t, err)
	require.Equal(t, "mine", got.Name)
}
