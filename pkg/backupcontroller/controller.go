/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package backupcontroller

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"k8s.io/klog/v2"
	protoetcd "sigs.k8s.io/etcd-manager/pkg/apis/etcd"
	"sigs.k8s.io/etcd-manager/pkg/backup"
	"sigs.k8s.io/etcd-manager/pkg/contextutil"
	"sigs.k8s.io/etcd-manager/pkg/etcd"
	"sigs.k8s.io/etcd-manager/pkg/etcdclient"
)

const loopInterval = time.Minute

type BackupController struct {
	clusterName string
	backupStore backup.Store

	dataDir string

	clientUrls []string

	etcdClientTLSConfig *tls.Config
	// lastBackup is the time at which we last performed a backup (as leader)
	lastBackup time.Time

	backupInterval time.Duration

	backupCleanup *BackupCleanup
}

func NewBackupController(backupStore backup.Store, clusterName string, clientUrls []string, etcdClientTLSConfig *tls.Config, dataDir string, backupInterval time.Duration) (*BackupController, error) {
	if clusterName == "" {
		return nil, fmt.Errorf("ClusterName is required")
	}

	m := &BackupController{
		clusterName:         clusterName,
		backupStore:         backupStore,
		dataDir:             dataDir,
		clientUrls:          clientUrls,
		etcdClientTLSConfig: etcdClientTLSConfig,
		backupInterval:      backupInterval,
		backupCleanup:       NewBackupCleanup(backupStore),
	}
	return m, nil
}

func (m *BackupController) Run(ctx context.Context) {
	contextutil.Forever(ctx,
		loopInterval, // We do our own sleeping
		func() {
			err := m.run(ctx)
			if err != nil {
				klog.Warningf("unexpected error running backup controller loop: %v", err)
			}
		})
}

func (m *BackupController) run(ctx context.Context) error {
	klog.V(2).Infof("starting backup controller iteration")

	etcdClient, err := etcdclient.NewClient(m.clientUrls, m.etcdClientTLSConfig)
	if err != nil {
		return fmt.Errorf("unable to reach etcd on %s: %v", m.clientUrls, err)
	}

	etcdVersion, err := etcdClient.ServerVersion(ctx)
	if err != nil {
		return fmt.Errorf("unable to find server version of etcd on %s: %v", m.clientUrls, err)
	}

	members, err := etcdClient.ListMembers(ctx)
	if err != nil {
		etcdclient.LoggedClose(etcdClient)
		return fmt.Errorf("unable to list members on %s: %v", m.clientUrls, err)
	}

	self, err := etcdClient.LocalNodeInfo(ctx)
	etcdclient.LoggedClose(etcdClient)
	if err != nil {
		return fmt.Errorf("unable to get node state on %s: %v", m.clientUrls, err)
	}

	if !self.IsLeader {
		klog.V(2).Infof("Not leader, won't backup")
		return nil
	}

	return m.maybeBackup(ctx, etcdVersion, members)
}

func (m *BackupController) maybeBackup(ctx context.Context, etcdVersion string, members []*etcdclient.EtcdProcessMember) error {
	now := time.Now()

	shouldBackup := now.Sub(m.lastBackup) > m.backupInterval
	if !shouldBackup {
		return nil
	}

	backup, err := m.doClusterBackup(ctx, etcdVersion, members)
	if err != nil {
		return err
	}

	klog.Infof("took backup: %v", backup)
	m.lastBackup = now

	if err := m.backupCleanup.MaybeDoBackupMaintenance(ctx); err != nil {
		klog.Warningf("error during backup cleanup: %v", err)
	}

	return nil
}

func (m *BackupController) doClusterBackup(ctx context.Context, etcdVersion string, members []*etcdclient.EtcdProcessMember) (*protoetcd.DoBackupResponse, error) {
	info := &protoetcd.BackupInfo{
		ClusterSpec: &protoetcd.ClusterSpec{
			MemberCount: int32(len(members)),
			EtcdVersion: etcdVersion,
		},
		EtcdVersion: etcdVersion,
	}

	return etcd.DoBackup(m.backupStore, info, m.dataDir, m.clientUrls, m.etcdClientTLSConfig)
}
