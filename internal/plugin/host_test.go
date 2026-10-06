// Copyright 2026-present Orbit Contributors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package plugin

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orbit-projects/orbit-kubernetes-go/internal/wire"
	"google.golang.org/protobuf/types/known/anypb"
)

type lifecycleProbe struct {
	active  atomic.Int32
	overlap atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (p *lifecycleProbe) Activate(context.Context, []byte) error { return p.transition() }

func (p *lifecycleProbe) Deactivate(context.Context) error { return p.transition() }

func (p *lifecycleProbe) transition() error {
	if p.active.Add(1) != 1 {
		p.overlap.Store(true)
	}
	p.entered <- struct{}{}
	<-p.release
	p.active.Add(-1)
	return nil
}

func (*lifecycleProbe) Health(context.Context) (Health, error) {
	return Health{Status: wire.PluginHealth_STATUS_HEALTHY}, nil
}

func (*lifecycleProbe) Invoke(context.Context, string, *anypb.Any) (*anypb.Any, string) {
	return nil, "unimplemented"
}

func commandFrame(id uint64, kind wire.PluginCommand_Kind) *wire.CoreFrame {
	return &wire.CoreFrame{Payload: &wire.CoreFrame_Command{Command: &wire.PluginCommand{
		RequestId: id, Kind: kind,
	}}}
}

func TestLifecycleCommandsAreSerialized(t *testing.T) {
	probe := &lifecycleProbe{
		entered: make(chan struct{}, 2),
		release: make(chan struct{}, 2),
	}
	var commands sync.Mutex
	firstDone := make(chan struct{})
	go func() {
		dispatchSerialized(context.Background(), probe, commandFrame(1, wire.PluginCommand_KIND_ACTIVATE), &commands)
		close(firstDone)
	}()
	<-probe.entered

	secondDone := make(chan struct{})
	go func() {
		dispatchSerialized(context.Background(), probe, commandFrame(2, wire.PluginCommand_KIND_DEACTIVATE), &commands)
		close(secondDone)
	}()

	select {
	case <-probe.entered:
		t.Fatal("deactivation began before activation completed")
	case <-time.After(25 * time.Millisecond):
	}
	probe.release <- struct{}{}
	<-probe.entered
	probe.release <- struct{}{}
	<-firstDone
	<-secondDone
	if probe.overlap.Load() {
		t.Fatal("lifecycle operations overlapped")
	}
}
