package main

import (
	"context"
	"errors"
	"io"

	runnersdk "github.com/nodima-studio/nodima-sdk/go"
	runnerv1 "github.com/nodima-studio/nodima-sdk/runner/v1"
)

type passthrough struct{}

func (passthrough) Run(ctx context.Context, input runnersdk.Input, output runnersdk.Output) error {
	initialize, err := input.Next(ctx)
	if err != nil {
		return err
	}
	ready := runnerv1.NewMessage(runnerv1.MessageReady)
	ready.ExecutionID = initialize.ExecutionID
	ready.NodeID = initialize.NodeID
	if err := output.Emit(ctx, ready); err != nil {
		return err
	}
	for {
		message, err := input.Next(ctx)
		if errors.Is(err, io.EOF) {
			return err
		}
		if err != nil {
			return err
		}
		if message.Type == runnerv1.MessageInputEnd {
			completed := runnerv1.NewMessage(runnerv1.MessageCompleted)
			completed.ExecutionID = initialize.ExecutionID
			completed.NodeID = initialize.NodeID
			return output.Emit(ctx, completed)
		}
		if message.Type == runnerv1.MessageInputBatch {
			message.Type = runnerv1.MessageOutputBatch
			message.PortID = "output"
			if err := output.Emit(ctx, message); err != nil {
				return err
			}
		}
	}
}

func main() { runnersdk.Main(passthrough{}) }
