package webbetahost

import (
	"context"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

func awaitBrowserResult(ctx context.Context, id string, events <-chan webbeta.Event, emit func(webbeta.Event) error, stop func(error) error, block func(string), diagnostic *webbeta.DispatchDiagnosticError, ackWait, generationWait time.Duration) error {
	started, baseElapsed := time.Now(), diagnostic.ElapsedMS
	ackDeadline := time.NewTimer(ackWait)
	defer ackDeadline.Stop()
	ackTimeout := ackDeadline.C
	timeout := time.NewTimer(generationWait)
	defer timeout.Stop()
	for {
		select {
		case <-ctx.Done():
			return stop(ctx.Err())
		case <-ackTimeout:
			return stop(&webbeta.Failure{Code: "dispatch_ack_timeout", Sent: true})
		case <-timeout.C:
			return stop(context.DeadlineExceeded)
		case ev, ok := <-events:
			if !ok {
				return stop(&webbeta.Failure{Code: "event_channel_closed", Sent: true})
			}
			if ev.ID != id {
				continue
			}
			diagnostic.Observe(ev)
			diagnostic.ElapsedMS = baseElapsed + time.Since(started).Milliseconds()
			provider.RecordCancellationDiagnostic(ctx, *diagnostic)
			if ev.Kind == "phase" {
				if ev.Code == "dispatch_ack" {
					ackDeadline.Stop()
					ackTimeout = nil
				}
				continue
			}
			if ev.Kind == "error" {
				if ev.Code == "conversation_mismatch" && diagnostic.Ack && !diagnostic.SendAttempted && !ev.Sent {
					return webbeta.KnownUnsent(&webbeta.Failure{Code: ev.Code})
				}
				if ev.Code == "cancelled" && ctx.Err() != nil {
					return stop(ctx.Err())
				}
				block(ev.Code)
				if ev.Sent {
					return stop(&webbeta.Failure{Code: ev.Code, Sent: true})
				}
				return &webbeta.Failure{Code: ev.Code, Sent: true}
			}
			if ev.Kind == "done" && !diagnostic.Ack {
				return stop(&webbeta.Failure{Code: "dispatch_ack_missing", Sent: true})
			}
			if err := emit(ev); err != nil {
				return stop(err)
			}
			if ev.Kind == "done" {
				return nil
			}
		}
	}
}
