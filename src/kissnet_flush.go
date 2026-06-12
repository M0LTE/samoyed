//nolint:gochecknoglobals
package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Flush a departed KISS TCP client's queued frames.
 *
 * Description: Frames received from a KISS TCP client are appended to the
 *		transmit queue and then transmitted whenever the channel allows,
 *		with no further connection to the client that sent them.  If the
 *		client disconnects while frames are still waiting (easily a long
 *		time on a busy half-duplex channel), those orphaned frames would
 *		keep going out on the air on behalf of a host that no longer
 *		exists - confusing the stations they are addressed to and
 *		polluting the channel for the next client.
 *
 *		To fix that we keep a side table, like the ACKMODE one in
 *		kiss_ackmode.go, mapping each queued packet to the KISS TCP
 *		client (port + client index) it came from.  Entries are
 *		registered just before tq_append and removed at every terminal
 *		disposition of the packet, by the ackmode_take / ackmode_discard
 *		helpers which are already called at exactly those points.
 *
 *		When a client disconnects, kissnet_flush_client removes its
 *		not-yet-transmitting frames from the transmit queue.  A frame
 *		the transmit thread has already dequeued is left alone - we
 *		never cut off a transmission in progress.  Pending ACKMODE
 *		acknowledgements for flushed frames are dropped, not echoed,
 *		because the frames never went out.
 *
 *		Attribution is per client, so other clients (on the same TCP
 *		port or another) are unaffected.  Frames from the serial port /
 *		pseudo terminal (kps == nil) are never registered - that
 *		attachment does not "disconnect".
 *
 *---------------------------------------------------------------*/

import "sync"

// kissOrigin identifies the KISS TCP client that queued a frame.
type kissOrigin struct {
	kps    *kissport_status_s
	client int
}

var kissOriginMu sync.Mutex
var kissOriginPending = map[*packet_t]kissOrigin{}

// kiss_origin_register records that frame pp was queued by KISS TCP client
// (kps, client).  Call this BEFORE handing pp to tq_append, for the same
// reason as ackmode_register.  It is a no-op for the serial port / pseudo
// terminal (kps == nil).
func kiss_origin_register(pp *packet_t, kps *kissport_status_s, client int) {
	if kps == nil {
		return
	}

	kissOriginMu.Lock()
	kissOriginPending[pp] = kissOrigin{kps: kps, client: client}
	kissOriginMu.Unlock()
}

// kiss_origin_forget removes the origin entry for pp, if any.  This is called
// from ackmode_take and ackmode_discard, which between them cover every
// terminal disposition of a queued packet, so the table cannot grow without
// bound and is always cleared before ax25_delete.
func kiss_origin_forget(pp *packet_t) {
	kissOriginMu.Lock()
	delete(kissOriginPending, pp)
	kissOriginMu.Unlock()
}

// kiss_origin_is reports whether pp was queued by KISS TCP client (kps, client).
func kiss_origin_is(pp *packet_t, kps *kissport_status_s, client int) bool {
	kissOriginMu.Lock()
	var origin, ok = kissOriginPending[pp]
	kissOriginMu.Unlock()

	return ok && origin.kps == kps && origin.client == client
}

// kissnet_flush_client discards all frames queued by the given KISS TCP
// client which have not yet started transmitting.  Call this when the client
// disconnects.  Any pending ACKMODE acknowledgements for the flushed frames
// are dropped without being echoed - the frames never went out.
func kissnet_flush_client(kps *kissport_status_s, client int) {
	var removed = tq_flush_matching(func(pp *packet_t) bool {
		return kiss_origin_is(pp, kps, client)
	})

	if len(removed) == 0 {
		return
	}

	text_color_set(DW_COLOR_INFO)
	dw_printf("Discarding %d frame(s) still waiting to be transmitted for departed KISS client %d on TCP port %d.\n", len(removed), client, kps.tcp_port)

	for _, pp := range removed {
		ackmode_discard(pp) // never transmitted - drop any pending ACKMODE ack, do NOT echo it
		ax25_delete(pp)
	}
}
