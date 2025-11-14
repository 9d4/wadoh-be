package controller

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

const (
	sendMessageWorkerQueueSize = 100
	sendMessageWorkerCount     = 5
)

type MessageJob struct {
	To                types.JID
	Message           *waE2E.Message
	IgnoreGlobalQueue bool
	UseReceiverQueue  bool
	TypingDuration    time.Duration
	// Optional delay after sending the message
	DelayAfterSend time.Duration
}

type SendMessageWorker struct {
	cli *whatsmeow.Client
	log zerolog.Logger

	// For jobs that can be sent in parallel
	jobs chan *MessageJob
	// Ensure only one goroutine sends messages at a time.
	globalQueue chan *MessageJob

	// Ensure only one goroutine sends messages to a specific receiver at a time.
	receiverLock *GenericSafeMap[types.JID, *sync.Mutex]
}

func NewSendMessageWorker(cli *whatsmeow.Client, log zerolog.Logger) *SendMessageWorker {
	worker := &SendMessageWorker{
		cli:          cli,
		log:          log,
		jobs:         make(chan *MessageJob, sendMessageWorkerQueueSize),
		globalQueue:  make(chan *MessageJob, 10),
		receiverLock: NewGenericSafeMap[types.JID, *sync.Mutex](),
	}

	for range sendMessageWorkerCount {
		go worker.startWorker(worker.jobs)
	}

	// Always have one global lock worker
	go worker.startWorker(worker.globalQueue)

	return worker
}

func (w *SendMessageWorker) Enqueue(job *MessageJob) {
	if job.UseReceiverQueue {
		w.jobs <- job
		return
	}

	// Use global queue instead
	w.globalQueue <- job
}

func (w *SendMessageWorker) Stop() {
	close(w.jobs)
}

func (w *SendMessageWorker) startWorker(jobs chan *MessageJob) {
	for job := range jobs {
		if job == nil {
			// channel closed
			w.log.Debug().Msg("sendMessageWorker exiting")
			return
		}

		w.send(job)
	}
}

func (w *SendMessageWorker) send(job *MessageJob) {
	if _, err := w.cli.SendMessage(context.TODO(), job.To, job.Message); err != nil {
		w.log.Error().Caller().
			Err(err).
			Str("jid", w.cli.Store.ID.String()).
			Str("to", job.To.String()).
			Msg("unable to send message")
		return
	}

	sendTyping(w.cli, job.To, job.TypingDuration)

	w.log.Debug().
		Str("jid", w.cli.Store.ID.String()).
		Str("to", job.To.String()).
		Msg("message sent")

	if job.DelayAfterSend > 0 {
		time.Sleep(job.DelayAfterSend)
	}
}

func (w *SendMessageWorker) getReceiverLock(jid types.JID) *sync.Mutex {
	lock, ok := w.receiverLock.Get(jid)
	if !ok {
		newLock := &sync.Mutex{}
		w.receiverLock.Add(jid, newLock)
		return newLock
	}
	return lock
}
