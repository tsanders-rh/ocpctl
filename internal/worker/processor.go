package worker

import (
	"context"
	"fmt"

	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// JobProcessor processes jobs by type
type JobProcessor struct {
	config                          *Config
	store                           *store.Store
	createHandler                   *CreateHandler
	destroyHandler                  *DestroyHandler
	configureEFSHandler             *ConfigureEFSHandler
	provisionSharedStorageHandler   *ProvisionSharedStorageHandler
	unlinkSharedStorageHandler      *UnlinkSharedStorageHandler
	hibernateHandler                *HibernateHandler
	resumeHandler                   *ResumeHandler
}

// NewJobProcessor creates a new job processor
func NewJobProcessor(config *Config, st *store.Store) *JobProcessor {
	return &JobProcessor{
		config:                        config,
		store:                         st,
		createHandler:                 NewCreateHandler(config, st),
		destroyHandler:                NewDestroyHandler(config, st),
		configureEFSHandler:           NewConfigureEFSHandler(config, st),
		provisionSharedStorageHandler: NewProvisionSharedStorageHandler(config, st),
		unlinkSharedStorageHandler:    NewUnlinkSharedStorageHandler(config, st),
		hibernateHandler:              NewHibernateHandler(config, st),
		resumeHandler:                 NewResumeHandler(config, st),
	}
}

// Process processes a job based on its type
func (p *JobProcessor) Process(ctx context.Context, job *types.Job) error {
	switch job.JobType {
	case types.JobTypeCreate:
		return p.createHandler.Handle(ctx, job)

	case types.JobTypeDestroy:
		return p.destroyHandler.Handle(ctx, job)

	case types.JobTypeScaleWorkers:
		return fmt.Errorf("scale workers not implemented yet")

	case types.JobTypeJanitorDestroy:
		return p.destroyHandler.Handle(ctx, job)

	case types.JobTypeOrphanSweep:
		return fmt.Errorf("orphan sweep not implemented yet")

	case types.JobTypeConfigureEFS:
		return p.configureEFSHandler.Handle(ctx, job)

	case types.JobTypeProvisionSharedStorage:
		return p.provisionSharedStorageHandler.Handle(ctx, job)

	case types.JobTypeUnlinkSharedStorage:
		return p.unlinkSharedStorageHandler.Handle(ctx, job)

	case types.JobTypeHibernate:
		return p.hibernateHandler.Handle(ctx, job)

	case types.JobTypeResume:
		return p.resumeHandler.Handle(ctx, job)

	default:
		return fmt.Errorf("unknown job type: %s", job.JobType)
	}
}
