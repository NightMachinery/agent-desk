package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/NightMachinery/agent-desk/internal/keeper"
)

const waitingCompaction = "waiting-compaction"

var errWaitingForCompaction = errors.New("waiting for compaction")

// startCompaction runs outside the send queue's lock. A later queue pass
// handles delivery after the keeper reports a terminal result.
func (s *server) startCompaction(ctx context.Context, j job, state sendProgress) {
	id := j.to.Paseo
	s.sendMu.Lock()
	if s.compacting[id] {
		s.sendMu.Unlock()
		return
	}
	if s.compacting == nil {
		s.compacting = make(map[string]bool)
	}
	s.compacting[id] = true
	s.sendMu.Unlock()
	go func() {
		ready := s.waitForCompaction(ctx, j, state)
		s.sendMu.Lock()
		delete(s.compacting, id)
		s.sendMu.Unlock()
		if ready {
			s.wakeSendQueue()
		}
	}()
}

func (s *server) waitForCompaction(ctx context.Context, j job, state sendProgress) bool {
	a, _, _ := s.userApp()
	st, err := a.store()
	if err != nil {
		fmt.Fprintf(s.logger, "agent-desk: compaction: %v\n", err)
		return false
	}
	record := func() bool {
		marker := a.deliveryState(j.msg, "delivery-state", state)
		ok, err := st.AppendDeliveryIfCurrent(marker, state.ID)
		if err != nil {
			fmt.Fprintf(s.logger, "agent-desk: compaction state: %v\n", err)
			return false
		}
		if ok {
			state.ID = marker.ID
		}
		return ok
	}
	finish := func(note string) bool {
		if ctx.Err() != nil {
			return false
		}
		state.Status, state.CompactionDone, state.CompactionNote = "queued", true, note
		return record()
	}
	client, err := a.keeperClient()
	if err != nil {
		if state.CompactionRequestedAt == 0 {
			return finish("Compaction unavailable. The message was released for delivery.")
		}
		return false
	}
	if state.CompactionRequestedAt == 0 {
		// Persist intent before the POST. If the process stops before it
		// saves the returned ID, look for the request in the keeper first.
		state.CompactionRequestedAt = float64(time.Now().UnixMicro()) / 1e6
		if ctx.Err() != nil || !record() {
			return false
		}
		result, err := client.Compact(ctx, j.to.Paseo)
		switch {
		case errors.Is(err, keeper.ErrUnavailable) || keeperRejected(err):
			return finish("Could not start compaction. The message was released for delivery. " + err.Error())
		case result.State == "failed":
			return finish("Compaction failed. The message was released for delivery.")
		case result.ID != "":
			state.CompactionID = result.ID
			if !record() {
				return false
			}
		}
	}
	for ctx.Err() == nil {
		poll := client.Agent
		if state.CompactionID != "" {
			poll = func(ctx context.Context, id string) (keeper.Agent, error) {
				return client.Wait(ctx, id, state.CompactionID)
			}
		}
		agent, err := poll(ctx, j.to.Paseo)
		var apiErr keeper.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			return finish("The keeper no longer knows this agent. The message was released for delivery.")
		}
		if err == nil {
			var c *keeper.Compaction
			if agent.Cache != nil {
				c = agent.Cache.Compaction
			}
			if state.CompactionID == "" {
				// The POST acknowledgement was lost. A queued or sent
				// compaction is safe to wait for, even if it predates this
				// request. Only a recent terminal record can be ours.
				if c == nil || (c.State != "queued" && c.State != "sent" && c.RequestedAt < state.CompactionRequestedAt) {
					return finish("Could not confirm compaction. The message was released for delivery.")
				}
				state.CompactionID = c.ID
				if !record() {
					return false
				}
			}
			if c == nil || c.ID != state.CompactionID {
				return finish("Compaction was superseded or removed. The message was released for delivery.")
			}
			switch c.State {
			case "completed":
				return finish("")
			case "failed", "canceled":
				note := "Compaction " + c.State + ". The message was released for delivery."
				if c.Error != "" {
					note += " " + c.Error
				}
				return finish(note)
			}
		}
		// A transient socket failure must keep an accepted compaction on hold.
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	return false
}

func keeperRejected(err error) bool {
	var failure keeper.APIError
	return errors.As(err, &failure)
}

// keeperLock checks the keeper's current registration before sending. Opening
// an existing lock only avoids creating keeper state for an ordinary send.
func (a *App) keeperLock(ctx context.Context, id string) (func(), error) {
	noop := func() {}
	client, err := a.keeperClient()
	if err != nil {
		return noop, nil
	}
	agent, err := client.Agent(ctx, id)
	if err != nil || agent.LockPath == "" {
		return noop, nil
	}
	return existingFlock(ctx, agent.LockPath)
}

func existingFlock(ctx context.Context, path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return func() {}, nil
	}
	if err != nil {
		return nil, err
	}
	fd := int(f.Fd())
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		switch {
		case err == nil:
			return func() { _ = syscall.Flock(fd, syscall.LOCK_UN); _ = f.Close() }, nil
		case !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR):
			f.Close()
			return nil, err
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			f.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
