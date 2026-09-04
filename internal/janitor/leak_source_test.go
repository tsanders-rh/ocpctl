package janitor

import (
	"testing"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

func TestSelectOriginatingJob(t *testing.T) {
	job := func(id string, jt types.JobType, st types.JobStatus) *types.Job {
		return &types.Job{ID: id, JobType: jt, Status: st}
	}

	cases := []struct {
		name   string
		jobs   []*types.Job // newest-first, as ListByClusterID returns
		wantID string       // "" means nil
	}{
		{
			name:   "no jobs",
			jobs:   nil,
			wantID: "",
		},
		{
			name: "no lifecycle jobs (only post-configure/hibernate)",
			jobs: []*types.Job{
				job("j1", types.JobTypePostConfigure, types.JobStatusFailed),
				job("j2", types.JobTypeHibernate, types.JobStatusSucceeded),
			},
			wantID: "",
		},
		{
			name: "prefers most recent FAILED lifecycle job",
			jobs: []*types.Job{
				job("destroy-fail", types.JobTypeDestroy, types.JobStatusFailed),
				job("create-ok", types.JobTypeCreate, types.JobStatusSucceeded),
			},
			wantID: "destroy-fail",
		},
		{
			name: "skips newer non-lifecycle, finds failed create",
			jobs: []*types.Job{
				job("post", types.JobTypePostConfigure, types.JobStatusRunning),
				job("create-fail", types.JobTypeCreate, types.JobStatusFailed),
			},
			wantID: "create-fail",
		},
		{
			name: "no failed lifecycle -> falls back to newest lifecycle",
			jobs: []*types.Job{
				job("destroy-ok", types.JobTypeDestroy, types.JobStatusSucceeded),
				job("create-ok", types.JobTypeCreate, types.JobStatusSucceeded),
			},
			wantID: "destroy-ok",
		},
		{
			name: "janitor destroy counts as lifecycle",
			jobs: []*types.Job{
				job("janitor-fail", types.JobTypeJanitorDestroy, types.JobStatusFailed),
			},
			wantID: "janitor-fail",
		},
		{
			name: "newest failed wins over older failed",
			jobs: []*types.Job{
				job("newer-fail", types.JobTypeDestroy, types.JobStatusFailed),
				job("older-fail", types.JobTypeCreate, types.JobStatusFailed),
			},
			wantID: "newer-fail",
		},
		{
			name: "nil entries are ignored",
			jobs: []*types.Job{
				nil,
				job("create-fail", types.JobTypeCreate, types.JobStatusFailed),
			},
			wantID: "create-fail",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := selectOriginatingJob(tc.jobs)
			switch {
			case tc.wantID == "" && got != nil:
				t.Fatalf("expected nil, got %q", got.ID)
			case tc.wantID != "" && got == nil:
				t.Fatalf("expected %q, got nil", tc.wantID)
			case tc.wantID != "" && got != nil && got.ID != tc.wantID:
				t.Fatalf("expected %q, got %q", tc.wantID, got.ID)
			}
		})
	}
}
