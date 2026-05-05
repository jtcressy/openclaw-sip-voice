package runtime

import (
	"testing"

	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/config"
	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/protocol"
)

func TestStateUpsertAndRemoveCallMaintainsSnapshotCopies(t *testing.T) {
	cfg, err := config.ParseEnv([]string{
		"SIP_BIND_ADDR=127.0.0.1:5060",
		"SIP_ADVERTISE_ADDR=127.0.0.1:5060",
	})
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	state := NewState(cfg)

	state.UpsertCall(protocol.CallSummary{
		CallID:    "call_in_000001",
		Direction: protocol.CallDirectionInbound,
		State:     protocol.CallStateRinging,
	})
	state.UpsertCall(protocol.CallSummary{
		CallID:    "call_in_000001",
		Direction: protocol.CallDirectionInbound,
		State:     protocol.CallStateActive,
	})

	snapshot := state.Snapshot()
	if len(snapshot.ActiveCalls) != 1 {
		t.Fatalf("active calls = %d, want 1", len(snapshot.ActiveCalls))
	}
	if snapshot.ActiveCalls[0].State != protocol.CallStateActive {
		t.Fatalf("call state = %q, want active", snapshot.ActiveCalls[0].State)
	}

	snapshot.ActiveCalls[0].State = "mutated"
	if got := state.Snapshot().ActiveCalls[0].State; got != protocol.CallStateActive {
		t.Fatalf("snapshot mutation leaked into state: %q", got)
	}

	state.RemoveCall("call_in_000001")
	if got := len(state.Snapshot().ActiveCalls); got != 0 {
		t.Fatalf("active calls after remove = %d, want 0", got)
	}
}
