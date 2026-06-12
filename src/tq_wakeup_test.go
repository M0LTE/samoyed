package direwolf

/*
 * Regression hammer for the tq_append / tq_wait_while_empty lost-wakeup race.
 *
 * The producer used to read xmit_thread_is_waiting WITHOUT the wake mutex and
 * skip the Signal when it was false; the consumer checked emptiness and only
 * then registered as a waiter.  An append landing in that gap sent no wakeup
 * and the consumer slept forever on a non-empty queue — observed in the field
 * as a samoyed instance with its xmit thread parked in Wait and 9 frames
 * queued (driven by packet.net's Packet.LinkBench over net-sim).
 *
 * This test drives the REAL tq machinery: one consumer goroutine doing the
 * xmit thread's wait/peek/remove loop, one producer appending frames with
 * empty→non-empty transitions on every iteration (each append is consumed
 * before the next, maximising the racy window).  Pre-fix it reliably hangs
 * within a few hundred iterations (caught by the deadline); post-fix it runs
 * to completion under -race.
 */

import (
	"testing"
	"time"
)

func TestTqWaitWakeupNeverLost(t *testing.T) {
	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	ptt_init(cfg)
	tq_init(cfg)

	var channel = 0

	const frames = 2000
	var consumed = make(chan int, frames)
	var done = make(chan struct{})

	go func() {
		defer close(done)
		for n := 0; n < frames; {
			tq_wait_while_empty(channel)
			for {
				var pp = tq_remove(channel, TQ_PRIO_1_LO)
				if pp == nil {
					break
				}
				ax25_delete(pp)
				consumed <- n
				n++
			}
		}
	}()

	var deadline = time.After(30 * time.Second)
	var sent = 0
	for sent < frames {
		var pp = ax25_from_text("BENCHA>BENCHB:tq wakeup hammer", true)
		if pp == nil {
			t.Fatal("could not build test packet")
		}
		tq_append(channel, TQ_PRIO_1_LO, pp)
		sent++

		// Wait for it to be consumed before the next append, so every
		// iteration exercises a fresh empty→non-empty transition — the
		// exact shape of the lost-wakeup window.
		select {
		case <-consumed:
		case <-deadline:
			t.Fatalf("wakeup lost: consumer wedged with %d/%d frames consumed (the pre-fix race)", sent-1, frames)
		}
	}

	select {
	case <-done:
	case <-deadline:
		t.Fatal("consumer did not finish")
	}
}
