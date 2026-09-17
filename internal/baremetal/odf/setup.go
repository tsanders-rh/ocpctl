package odf

import (
	"context"
	"time"
)

// Setup runs the whole ODF-external feature in rhwa-lab's order: define the ceph
// VM, bootstrap ceph, export the external-cluster details, install the ODF
// operator, import the details, and create the external-mode StorageCluster.
// The caller (lifecycle) gates this on the profile's ODF being enabled.
func Setup(ctx context.Context, exec executor, spec Spec) error {
	return setup(ctx, exec, spec, time.Sleep)
}

func setup(ctx context.Context, exec executor, spec Spec, sleep func(time.Duration)) error {
	if err := DefineCephVM(ctx, exec, spec); err != nil {
		return err
	}
	if err := bootstrap(ctx, exec, spec, sleep); err != nil {
		return err
	}
	raw, err := Export(ctx, exec, spec)
	if err != nil {
		return err
	}
	if err := installOperator(ctx, exec, spec, sleep); err != nil {
		return err
	}
	if err := ImportExternal(ctx, exec, spec, raw); err != nil {
		return err
	}
	return createStorageCluster(ctx, exec, spec, sleep)
}
