// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package docker

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/platform/dockerapi"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/projects/infra"
)

// DestroyScope removes the Live scope once its owning Flow has stopped its
// workloads: every container, generation network and volume that carries the
// scope's owner labels, then the project network and the credentials the
// server generated for the project. It returns infra.ErrPending until
// everything is gone. The Sandbox of the project's Session is no part of the
// scope; while it is still attached, the network is not gone yet.
func (k *Infrastructure) DestroyScope(ctx context.Context, project string, refs infra.ScopeRefs) error {
	networkName := k.ProjectNetwork(project)
	network, err := k.CleanupNetwork(ctx, networkName, refs.Storage.Boundary.UID, k.OwnerLabels(project, infra.ComponentInfrastructure))
	if err != nil {
		return err
	}
	// The scope's objects all carry its Live owner labels, whichever
	// component owns them, and this Native instance's label.
	scope := map[string]string{deploymentnames.ManagedByLabel: k.names.Product, deploymentnames.PartOfLabel: k.names.Product + "-live", deploymentnames.ProjectLabel: project, NativeInstanceLabel: k.config.Instance}
	containers, err := k.api.ListContainers(ctx, dockerapi.Labels(SelectorLabels(scope)))
	if err != nil {
		return err
	}
	for _, container := range containers {
		if VerifyLabels(container.Labels, scope) != nil {
			continue
		}
		name := strings.TrimPrefix(firstName(container.Names), "/")
		if err = pending(k.RemoveContainer(ctx, name, scope, componentStopGrace))("container " + name); err != nil {
			return err
		}
	}
	generations := WithLabel(scope, deploymentnames.ComponentLabel, infra.ComponentApplication.String())
	networks, err := k.api.ListNetworks(ctx, dockerapi.Labels(SelectorLabels(generations)))
	if err != nil {
		return err
	}
	for _, generation := range networks {
		if VerifyLabels(generation.Labels, generations) != nil {
			continue
		}
		if err = pending(k.RemoveNetwork(ctx, generation.Name, generation.ID, scope))("network " + generation.Name); err != nil {
			return err
		}
	}
	volumes, err := k.api.ListVolumes(ctx, dockerapi.Labels(SelectorLabels(scope)))
	if err != nil {
		return err
	}
	for _, volume := range volumes {
		if VerifyLabels(volume.Labels, scope) != nil {
			continue
		}
		if err = pending(k.RemoveVolume(ctx, volume.Name, scope))("volume " + volume.Name); err != nil {
			return err
		}
	}
	if network != nil {
		gone, removeErr := k.RemoveNetwork(ctx, networkName, network.ID, k.OwnerLabels(project, infra.ComponentInfrastructure))
		if removeErr != nil {
			return removeErr
		}
		if !gone {
			return fmt.Errorf("%w: network %s still has attached containers", infra.ErrPending, networkName)
		}
	}
	// The project's routes have no target left; their holders are gone.
	for _, scope := range []infra.ScopeKind{infra.ScopePreview, infra.ScopeLive} {
		if _, err = k.routes.Withdraw(ctx, k.config.Routes.Host(scope, project), func(current Route) bool { return current.Owner.ProjectID == project }); err != nil {
			return err
		}
	}
	if err = os.RemoveAll(k.ProjectCredentials(project)); err != nil {
		return fmt.Errorf("remove the project's credentials: %w", err)
	}
	return nil
}

// pending turns the answer of one removal into nil when the object is gone,
// infra.ErrPending naming it while it is being removed, or the removal's error.
func pending(gone bool, err error) func(what string) error {
	return func(what string) error {
		switch {
		case err != nil:
			return err
		case !gone:
			return fmt.Errorf("%w: %s is being removed", infra.ErrPending, what)
		default:
			return nil
		}
	}
}

// firstName is the first name of a listed container.
func firstName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

// DestroyPreviewConfiguration deletes every object version below the
// project's Preview prefix in the Live bucket, in bounded batches, once its
// Preview ended. It returns infra.ErrPending until the prefix is empty. An
// absent project network took the storage with it, because Live cleanup
// removes the network last.
func (k *Infrastructure) DestroyPreviewConfiguration(ctx context.Context, project string, storage infra.StorageRefs) error {
	network, err := k.CleanupNetwork(ctx, k.ProjectNetwork(project), storage.Boundary.UID, k.OwnerLabels(project, infra.ComponentInfrastructure))
	if err != nil || network == nil {
		return err
	}
	minio, err := k.openStorage(ctx, project, network.ID, infra.ResourceRef{})
	if err != nil {
		return err
	}
	objects := minio.objects
	store := comm.Must(objectstore.NewStore(infra.ConfigurationS3Client(objects), objects.Bucket, "", strings.TrimSuffix(infra.ConfigurationPrefix, "/"), true))
	done, err := store.DeletePrefix(ctx, strings.TrimPrefix(infra.PreviewKeysOf(project).ConfigurationPrefix, infra.ConfigurationPrefix))
	if err != nil || done {
		return err
	}
	return fmt.Errorf("%w: the Preview configuration of project %s is being deleted", infra.ErrPending, project)
}

// DestroyPreviewGrants deletes every storage grant issued to the project's
// Preview, revoked fences included, once the Preview ended: the MinIO
// accounts its grants marked, one per call, each after its credential file,
// which the next call finds again by the account if the answer was lost, and
// then the Preview's remaining credential files. It returns infra.ErrPending
// until they are gone. An absent project network took the accounts with it.
func (k *Infrastructure) DestroyPreviewGrants(ctx context.Context, project string, storage infra.StorageRefs, _ string) error {
	network, err := k.CleanupNetwork(ctx, k.ProjectNetwork(project), storage.Boundary.UID, k.OwnerLabels(project, infra.ComponentInfrastructure))
	if err != nil {
		return err
	}
	if network != nil {
		minio, openErr := k.openStorage(ctx, project, network.ID, infra.ResourceRef{})
		if openErr != nil {
			return openErr
		}
		accounts, listErr := minio.admin.ListServiceAccounts(ctx, minio.root)
		if listErr != nil {
			return fmt.Errorf("list the Live storage accounts: %w", listErr)
		}
		keys := infra.PreviewKeysOf(project)
		for _, account := range accounts.Accounts {
			grant, marked := keys.MarkedGrant(account.Description)
			if !marked {
				continue
			}
			if err = k.destroyGrant(ctx, minio, project, grant, account.AccessKey); err != nil {
				return err
			}
			return fmt.Errorf("%w: the Preview storage grant %s of project %s is being deleted", infra.ErrPending, grant, project)
		}
	}
	files, err := k.previewGrantFiles(project)
	if err != nil {
		return err
	}
	for path, owner := range files {
		if err = removeCredential(path, owner); err != nil {
			return err
		}
	}
	return nil
}

// destroyGrant deletes one Preview grant generation's credential file and
// then its MinIO account, which marks the grant, so a lost answer leaves the
// account for the next call to find. The account must be the root user's.
func (k *Infrastructure) destroyGrant(ctx context.Context, minio minioConnection, project, grant, access string) error {
	files, err := k.previewGrantFiles(project)
	if err != nil {
		return err
	}
	path := k.grantCredentialPath(project, grant)
	if owner, found := files[path]; found {
		if err = removeCredential(path, owner); err != nil {
			return err
		}
	}
	info, err := minio.admin.InfoServiceAccount(ctx, access)
	switch {
	case infra.MissingStorageAccount(err):
		return nil
	case err != nil:
		return fmt.Errorf("read the storage account of %s: %w", grant, err)
	case info.ParentUser != minio.root:
		return fmt.Errorf("%w: the storage account of %s has another parent", infra.ErrOwnershipConflict, grant)
	}
	if err = minio.admin.DeleteServiceAccount(ctx, access); err != nil {
		return fmt.Errorf("delete the storage account of %s: %w", grant, err)
	}
	return nil
}
