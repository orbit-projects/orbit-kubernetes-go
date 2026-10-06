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
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/orbit-projects/orbit-kubernetes-go/internal/wire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	grpcmetadata "google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	maxBootstrap = 64 * 1024
	maxMessage   = 1088 * 1024
	maxConfig    = 1024 * 1024
	maxCalls     = 16
)

var safeCode = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)

type metadata struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Version              string   `json:"version"`
	APIVersion           string   `json:"api_version"`
	Dependencies         []string `json:"dependencies"`
	OptionalDependencies []string `json:"optional_dependencies"`
	Capabilities         []string `json:"capabilities"`
	RequiredCapabilities []string `json:"required_capabilities"`
}

type bootstrap struct {
	Endpoint string   `json:"endpoint"`
	Token    string   `json:"token"`
	Metadata metadata `json:"metadata"`
}

// Health is the bounded runtime health result returned to Core.
type Health struct {
	Status  wire.PluginHealth_Status
	Message string
}

// Handler contains the provider's lifecycle and capability implementation.
type Handler interface {
	Activate(context.Context, []byte) error
	Deactivate(context.Context) error
	Health(context.Context) (Health, error)
	Invoke(context.Context, string, *anypb.Any) (*anypb.Any, string)
}

// Run authenticates this provider to the Python Core process host and supervises its lifecycle.
func Run(ctx context.Context, r io.Reader, implementation Handler) error {
	boot, err := readBootstrap(r)
	if err != nil {
		return err
	}
	token, _ := hex.DecodeString(boot.Token)
	conn, err := grpc.NewClient(boot.Endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxMessage), grpc.MaxCallSendMsgSize(maxMessage)),
	)
	if err != nil {
		return errors.New("could not initialize Core connection")
	}
	defer conn.Close()
	streamCtx := metadataOutgoing(ctx, hex.EncodeToString(token))
	stream, err := wire.NewProcessPluginClient(conn).Connect(streamCtx)
	if err != nil {
		return errors.New("could not connect to Core process host")
	}
	if err := stream.Send(&wire.PluginFrame{Payload: &wire.PluginFrame_Hello{Hello: &wire.PluginHello{
		ProtocolMajor: 1, ProtocolMinor: 0, Metadata: wireMetadata(boot.Metadata),
	}}}); err != nil {
		return errors.New("could not send Core process handshake")
	}

	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var sends sync.Mutex
	var commands sync.Mutex
	var workers sync.WaitGroup
	gate := make(chan struct{}, maxCalls)
	for {
		frame, err := stream.Recv()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return errors.New("Core process connection ended")
		}
		if _, isShutdown := frame.GetPayload().(*wire.CoreFrame_Shutdown); isShutdown {
			cancel()
			finished := make(chan struct{})
			go func() { workers.Wait(); close(finished) }()
			select {
			case <-finished:
				return nil
			case <-time.After(3 * time.Second):
				return errors.New("provider operations exceeded the shutdown deadline")
			}
		}
		select {
		case gate <- struct{}{}:
		case <-workCtx.Done():
			return workCtx.Err()
		}
		workers.Add(1)
		go func(request *wire.CoreFrame) {
			defer workers.Done()
			defer func() { <-gate }()
			response := dispatchSerialized(workCtx, implementation, request, &commands)
			if response == nil || workCtx.Err() != nil {
				return
			}
			sends.Lock()
			defer sends.Unlock()
			if stream.Send(response) != nil {
				cancel()
			}
		}(frame)
	}
}

func dispatchSerialized(ctx context.Context, implementation Handler, frame *wire.CoreFrame, commands *sync.Mutex) *wire.PluginFrame {
	if _, isCommand := frame.GetPayload().(*wire.CoreFrame_Command); isCommand {
		commands.Lock()
		defer commands.Unlock()
	}
	return dispatch(ctx, implementation, frame)
}

func readBootstrap(r io.Reader) (bootstrap, error) {
	var value bootstrap
	line, err := bufio.NewReader(io.LimitReader(r, maxBootstrap+1)).ReadBytes('\n')
	if err != nil || len(line) > maxBootstrap {
		return value, errors.New("invalid Orbit plugin bootstrap")
	}
	decoder := json.NewDecoder(strings.NewReader(string(line)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
		return bootstrap{}, errors.New("invalid Orbit plugin bootstrap")
	}
	host, port, err := net.SplitHostPort(value.Endpoint)
	if err != nil || host != "127.0.0.1" || port == "" {
		return bootstrap{}, errors.New("invalid Orbit plugin endpoint")
	}
	if _, err := net.LookupPort("tcp", port); err != nil {
		return bootstrap{}, errors.New("invalid Orbit plugin endpoint")
	}
	secret, err := hex.DecodeString(value.Token)
	if err != nil || len(secret) != 32 {
		return bootstrap{}, errors.New("invalid Orbit plugin credential")
	}
	if value.Metadata.ID == "" || value.Metadata.Name == "" || value.Metadata.Version == "" || value.Metadata.APIVersion == "" {
		return bootstrap{}, errors.New("invalid Orbit plugin metadata")
	}
	return value, nil
}

func metadataOutgoing(ctx context.Context, token string) context.Context {
	return grpcmetadata.NewOutgoingContext(ctx, grpcmetadata.Pairs("authorization", "Bearer "+token))
}

func wireMetadata(value metadata) *wire.PluginMetadata {
	return &wire.PluginMetadata{
		Id: value.ID, Name: value.Name, Version: value.Version, ApiVersion: value.APIVersion,
		Dependencies: value.Dependencies, OptionalDependencies: value.OptionalDependencies,
		Capabilities: value.Capabilities, RequiredCapabilities: value.RequiredCapabilities,
	}
}

func dispatch(ctx context.Context, implementation Handler, frame *wire.CoreFrame) (out *wire.PluginFrame) {
	defer func() {
		if recover() != nil {
			out = failureFrame(frame)
		}
	}()
	switch payload := frame.GetPayload().(type) {
	case *wire.CoreFrame_Command:
		command := payload.Command
		if command == nil || command.RequestId == 0 || len(command.ConfigurationJson) > maxConfig {
			return failedCommand(command.GetRequestId(), "invalid-command")
		}
		var err error
		switch command.Kind {
		case wire.PluginCommand_KIND_ACTIVATE:
			err = implementation.Activate(ctx, append([]byte(nil), command.ConfigurationJson...))
		case wire.PluginCommand_KIND_DEACTIVATE:
			err = implementation.Deactivate(ctx)
		default:
			err = errors.New("unsupported command")
		}
		if err != nil {
			return failedCommand(command.RequestId, "failed")
		}
		return &wire.PluginFrame{Payload: &wire.PluginFrame_CommandResult{CommandResult: &wire.CommandResult{
			RequestId: command.RequestId, Succeeded: true,
		}}}
	case *wire.CoreFrame_HealthProbe:
		probe := payload.HealthProbe
		if probe == nil || probe.RequestId == 0 {
			return nil
		}
		result, err := implementation.Health(ctx)
		if err != nil || result.Status < wire.PluginHealth_STATUS_HEALTHY || result.Status > wire.PluginHealth_STATUS_UNHEALTHY {
			result = Health{Status: wire.PluginHealth_STATUS_UNHEALTHY, Message: "Provider health check failed."}
		}
		return &wire.PluginFrame{Payload: &wire.PluginFrame_Health{Health: &wire.PluginHealth{
			RequestId: probe.RequestId, Status: result.Status, Message: safeMessage(result.Message),
		}}}
	case *wire.CoreFrame_CapabilityCall:
		call := payload.CapabilityCall
		if call == nil || call.RequestId == 0 || call.Request == nil {
			return nil
		}
		response, code := implementation.Invoke(ctx, call.Method, call.Request)
		if code != "" {
			if !safeCode.MatchString(code) {
				code = "failed"
			}
			return &wire.PluginFrame{Payload: &wire.PluginFrame_CapabilityResult{CapabilityResult: &wire.CapabilityResult{
				RequestId: call.RequestId, ErrorCode: code,
			}}}
		}
		if response == nil || proto.Size(response) > maxConfig {
			code = "invalid-response"
			return &wire.PluginFrame{Payload: &wire.PluginFrame_CapabilityResult{CapabilityResult: &wire.CapabilityResult{
				RequestId: call.RequestId, ErrorCode: code,
			}}}
		}
		return &wire.PluginFrame{Payload: &wire.PluginFrame_CapabilityResult{CapabilityResult: &wire.CapabilityResult{
			RequestId: call.RequestId, Response: response,
		}}}
	default:
		return nil
	}
}

func failureFrame(frame *wire.CoreFrame) *wire.PluginFrame {
	switch payload := frame.GetPayload().(type) {
	case *wire.CoreFrame_Command:
		if payload.Command == nil || payload.Command.RequestId == 0 {
			return nil
		}
		return failedCommand(payload.Command.RequestId, "internal")
	case *wire.CoreFrame_HealthProbe:
		if payload.HealthProbe == nil || payload.HealthProbe.RequestId == 0 {
			return nil
		}
		return &wire.PluginFrame{Payload: &wire.PluginFrame_Health{Health: &wire.PluginHealth{
			RequestId: payload.HealthProbe.RequestId,
			Status:    wire.PluginHealth_STATUS_UNHEALTHY,
			Message:   "Provider health check failed.",
		}}}
	case *wire.CoreFrame_CapabilityCall:
		if payload.CapabilityCall == nil || payload.CapabilityCall.RequestId == 0 {
			return nil
		}
		return &wire.PluginFrame{Payload: &wire.PluginFrame_CapabilityResult{CapabilityResult: &wire.CapabilityResult{
			RequestId: payload.CapabilityCall.RequestId,
			ErrorCode: "internal",
		}}}
	default:
		return nil
	}
}

func failedCommand(id uint64, code string) *wire.PluginFrame {
	return &wire.PluginFrame{Payload: &wire.PluginFrame_CommandResult{CommandResult: &wire.CommandResult{
		RequestId: id, ErrorCode: code, ErrorMessage: "Provider operation failed.",
	}}}
}

func safeMessage(value string) string {
	var result strings.Builder
	for _, char := range value {
		if unicode.IsControl(char) {
			continue
		}
		result.WriteRune(char)
		if result.Len() >= 256 {
			break
		}
	}
	return strings.TrimSpace(result.String())
}
