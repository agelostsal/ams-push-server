package push

import (
	"context"
	"github.com/ARGOeu/ams-push-server/consumers"
	"github.com/ARGOeu/ams-push-server/senders"
	"time"

	amsPb "github.com/ARGOeu/ams-push-server/api/v1/grpc/proto"
	log "github.com/sirupsen/logrus"
)

// LinearWorker implements a worker that follows a linear strategy when trying to push
type LinearWorker struct {
	sub              *amsPb.Subscription
	consumer         consumers.Consumer
	sender           senders.Sender
	cancel           context.CancelFunc
	ctx              context.Context
	deactivationChan chan<- consumers.CancelableError
}

// NewLinearWorker initialises and configures a new linear worker
func NewLinearWorker(sub *amsPb.Subscription, c consumers.Consumer, s senders.Sender, ch chan<- consumers.CancelableError) *LinearWorker {
	lw := new(LinearWorker)

	parentCtx := context.TODO()
	ctx, cancel := context.WithCancel(parentCtx)

	lw.sub = sub
	lw.consumer = c
	lw.sender = s
	lw.ctx = ctx
	lw.cancel = cancel
	lw.deactivationChan = ch

	return lw
}

// Subscription returns the currently active subscription inside the linear worker
func (w *LinearWorker) Subscription() *amsPb.Subscription {
	return w.sub
}

// Start starts the push functionality for the worker
func (w *LinearWorker) Start() {

	timer := time.NewTimer(0)
	rate := time.Duration(w.sub.PushConfig.RetryPolicy.Period)

Loop:
	for {
		select {
		case <-timer.C:
			w.push()
		case <-w.ctx.Done():
			canceled := timer.Stop()

			if !canceled {
				<-timer.C
			}

			break Loop
		}

		timer.Reset(rate * time.Millisecond)
	}
}

// push executes the push cycle of consume -> send -> ack
func (w *LinearWorker) push() {

	rml, err := w.consumer.Consume(w.ctx)
	if err != nil {

		ce, ok := w.consumer.ToCancelableError(err)
		if ok {
			w.deactivationChan <- ce
			return
		}

		if err.Error() == "no new messages" {
			log.WithFields(
				log.Fields{
					"type":     "service_log",
					"resource": w.consumer.ResourceInfo(),
				},
			).Debug("No new messages")
			return
		}

		log.WithFields(
			log.Fields{
				"type":     "service_log",
				"resource": w.consumer.ResourceInfo(),
				"error":    err.Error(),
			},
		).Error("Could not consume message")
		return
	}

	pm := senders.PushMsg{
		Sub: w.consumer.ResourceInfo(),
		Msg: rml.RecMsgs[0].Msg,
	}

	err = w.sender.Send(w.ctx, pm)
	if err != nil {
		return
	}

	err = w.consumer.Ack(w.ctx, rml.RecMsgs[0].AckID)
	if err != nil {
		return
	}

}

// Stop stops the push worker's functionality
func (w *LinearWorker) Stop() {
	w.cancel()
}