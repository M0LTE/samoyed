package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// queueTestFrame builds a packet and queues it the same way kiss_process_msg
// does for an ordinary (non-ACKMODE) KISS data frame from a TCP client.
func queueTestFrame(t *testing.T, channel int, kps *kissport_status_s, client int) *packet_t {
	t.Helper()

	var pp = ax25_from_text("Q1TEST>Q2TEST:disconnect flush test", true)
	require.NotNil(t, pp)

	kiss_origin_register(pp, kps, client)
	tq_append(channel, TQ_PRIO_1_LO, pp)

	return pp
}

func TestKissnetFlush_DropsDepartedClientsFrames(t *testing.T) {
	setupAckmodeEnv(t)

	var kps = new(kissport_status_s)
	queueTestFrame(t, 0, kps, 0)
	queueTestFrame(t, 0, kps, 0)

	require.Equal(t, 2, tq_count(0, TQ_PRIO_1_LO, "", "", false))

	kissnet_flush_client(kps, 0)

	assert.Equal(t, 0, tq_count(0, TQ_PRIO_1_LO, "", "", false), "departed client's queued frames must be flushed")
	assert.Nil(t, tq_remove(0, TQ_PRIO_1_LO))
}

func TestKissnetFlush_LeavesOtherClientsFrames(t *testing.T) {
	setupAckmodeEnv(t)

	var kps = new(kissport_status_s)
	var otherKps = new(kissport_status_s)

	queueTestFrame(t, 0, kps, 0)      // departing client
	queueTestFrame(t, 0, kps, 1)      // different client, same TCP port
	queueTestFrame(t, 0, otherKps, 0) // different TCP port

	// Serial port / pseudo terminal (kps == nil) - never registered, never flushed.
	var serialPP = ax25_from_text("Q1TEST>Q2TEST:serial frame", true)
	require.NotNil(t, serialPP)
	kiss_origin_register(serialPP, nil, -1)
	tq_append(0, TQ_PRIO_1_LO, serialPP)

	require.Equal(t, 4, tq_count(0, TQ_PRIO_1_LO, "", "", false))

	kissnet_flush_client(kps, 0)

	assert.Equal(t, 3, tq_count(0, TQ_PRIO_1_LO, "", "", false), "only the departed client's frames may be flushed")

	// Clean up remaining queue entries.
	for {
		var pp = tq_remove(0, TQ_PRIO_1_LO)
		if pp == nil {
			break
		}
		ackmode_discard(pp)
		ax25_delete(pp)
	}
}

// A flushed ACKMODE frame must never be echoed - the frame did not transmit.
func TestKissnetFlush_DropsPendingAckmodeAckWithoutEcho(t *testing.T) {
	setupAckmodeEnv(t)

	var rec sendfunRecorder
	var kps = new(kissport_status_s)

	var pp = ax25_from_text("Q1TEST>Q2TEST:ackmode flush test", true)
	require.NotNil(t, pp)

	ackmode_register(pp, [2]byte{0xAA, 0xBB}, 0, rec.fn, kps, 0)
	kiss_origin_register(pp, kps, 0)
	tq_append(0, TQ_PRIO_1_LO, pp)

	kissnet_flush_client(kps, 0)

	assert.Equal(t, 0, tq_count(0, TQ_PRIO_1_LO, "", "", false))
	assert.Empty(t, rec.calls, "flushed ACKMODE frame must not be echoed")

	// And the pending entry is really gone, not just unsent.
	ackmode_notify_sent(pp)
	assert.Empty(t, rec.calls)
}

// The terminal-disposition helpers must clear the origin table, so a frame
// that transmits (or is dropped) before the disconnect cannot be matched by a
// later flush, and the table cannot grow without bound.
func TestKissnetFlush_OriginForgottenAtTerminalDisposition(t *testing.T) {
	var kps = new(kissport_status_s)

	var taken = new(packet_t)
	kiss_origin_register(taken, kps, 3)
	require.True(t, kiss_origin_is(taken, kps, 3))
	ackmode_take(taken) // transmitted
	assert.False(t, kiss_origin_is(taken, kps, 3))

	var dropped = new(packet_t)
	kiss_origin_register(dropped, kps, 3)
	ackmode_discard(dropped) // dropped
	assert.False(t, kiss_origin_is(dropped, kps, 3))
}

func TestKissnetFlush_NoFramesIsHarmless(t *testing.T) {
	setupAckmodeEnv(t)

	kissnet_flush_client(new(kissport_status_s), 0)
}
