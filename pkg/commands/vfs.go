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

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"k8s.io/klog/v2"
	"k8s.io/kops/util/pkg/vfs"
	protoetcd "sigs.k8s.io/etcd-manager/pkg/apis/etcd"
)

const EtcdClusterCreated = "etcd-cluster-created"
const EtcdClusterSpec = "etcd-cluster-spec"

func NewVFSStore(p vfs.Path) (Store, error) {
	s := &vfsStore{
		commandsBase: p.Join("control"),
	}
	return s, nil
}

type vfsStore struct {
	commandsBase vfs.Path
}

var _ Store = &vfsStore{}

type vfsCommand struct {
	p    vfs.Path
	data protoetcd.Command
}

var _ Command = &vfsCommand{}

func (c *vfsCommand) Data() protoetcd.Command {
	return c.data
}

func (s *vfsStore) AddCommand(cmd *protoetcd.Command) error {
	ctx := context.TODO()

	sequence := "000000"
	now := time.Now()

	cmd.Timestamp = now.UnixNano()

	name := now.UTC().Format(time.RFC3339) + "-" + sequence

	// Save the command file
	{
		data, err := protoetcd.ToJson(cmd)
		if err != nil {
			return fmt.Errorf("error marshalling command: %v", err)
		}

		p := s.commandsBase.Join(name, CommandFilename)
		klog.Infof("Adding command at %s: %v", p, cmd)
		if err := p.WriteFile(ctx, bytes.NewReader([]byte(data)), nil); err != nil {
			return fmt.Errorf("error writing file %q: %v", p.Path(), err)
		}
	}

	return nil
}

func (s *vfsStore) ListCommands() ([]Command, error) {
	ctx := context.TODO()

	files, err := s.commandsBase.ReadTree(ctx)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("error reading %s: %v", s.commandsBase.Path(), err)
	}

	var commands []Command
	for _, f := range files {
		if f.Base() != CommandFilename {
			continue
		}

		data, err := f.ReadFile(ctx)
		if err != nil {
			return nil, fmt.Errorf("error reading %s: %v", f, err)
		}

		command := &vfsCommand{}
		if err = protoetcd.FromJson(string(data), &command.data); err != nil {
			return nil, fmt.Errorf("error parsing command %q: %v", f, err)
		}
		command.p = f

		klog.Infof("read command for %q: %v", f, command.data.String())

		commands = append(commands, command)
	}

	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Data().Timestamp < commands[j].Data().Timestamp
	})

	klog.V(2).Infof("listed commands in %s: %d commands", s.commandsBase.Path(), len(commands))

	return commands, nil
}

func (s *vfsStore) RemoveCommand(command Command) error {
	p := command.(*vfsCommand).p
	ctx := context.TODO()
	klog.Infof("deleting command %s", p)

	if err := p.Remove(ctx); err != nil {
		return fmt.Errorf("error removing command %s: %v", p, err)
	}

	return nil
}

func (s *vfsStore) GetExpectedClusterSpec() (*protoetcd.ClusterSpec, error) {
	ctx := context.TODO()

	p := s.commandsBase.Join(EtcdClusterSpec)
	data, err := p.ReadFile(ctx)
	if err != nil {
		if os.IsNotExist(err) {
			// TODO: On S3, loop to try to establish consistency?
			return nil, nil
		}
		return nil, fmt.Errorf("error reading cluster spec file %s: %v", p.Path(), err)
	}

	spec := &protoetcd.ClusterSpec{}
	if err = protoetcd.FromJson(string(data), spec); err != nil {
		return nil, fmt.Errorf("error parsing cluster spec %s: %v", p.Path(), err)
	}

	return spec, nil
}

func (s *vfsStore) SetExpectedClusterSpec(spec *protoetcd.ClusterSpec) error {
	ctx := context.TODO()

	data, err := protoetcd.ToJson(spec)
	if err != nil {
		return fmt.Errorf("error serializing cluster spec: %v", err)
	}

	p := s.commandsBase.Join(EtcdClusterSpec)
	if err := p.WriteFile(ctx, bytes.NewReader([]byte(data)), nil); err != nil {
		return fmt.Errorf("error writing cluster spec file %s: %v", p.Path(), err)
	}

	return nil
}

func (s *vfsStore) IsNewCluster() (bool, error) {
	ctx := context.TODO()

	p := s.commandsBase.Join(EtcdClusterCreated)

	// Note that we use ReadFile so that we use a GET on S3, which is more consistent
	data, err := p.ReadFile(ctx)
	if err != nil {
		if os.IsNotExist(err) {
			// TODO: On S3, loop to try to establish consistency?
			return true, nil
		}
		return false, fmt.Errorf("error reading cluster-creation marker file %s: %v", p.Path(), err)
	}
	if len(data) == 0 {
		return false, fmt.Errorf("marker file %s was unexpectedly empty: %v", p.Path(), err)
	}
	return false, nil
}

type etcdClusterCreated struct {
	Timestamp int64 `json:"timestamp,omitempty"`
}

func (s *vfsStore) MarkClusterCreated() error {
	ctx := context.TODO()

	d := &etcdClusterCreated{
		Timestamp: time.Now().UnixNano(),
	}

	data, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("error serializing cluster-creation marker file: %v", err)
	}

	p := s.commandsBase.Join(EtcdClusterCreated)
	if err := p.WriteFile(ctx, bytes.NewReader([]byte(data)), nil); err != nil {
		return fmt.Errorf("error creating cluster-creation marker file %s: %v", p.Path(), err)
	}
	return nil
}
