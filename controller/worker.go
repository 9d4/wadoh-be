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
	sendMessageWorkerQueueSize       = 100
	sendMessageWorkerJobsConcurrency = 5
	minDefaultTypingSecond           = 1
	maxDefaultTypingSecond           = 3
	recvQueueIdleTimeout             = 2 * time.Second
)

type MessageJob struct {
	To                types.JID
	Message           *waE2E.Message
	IgnoreGlobalQueue bool
	UseReceiverQueue  bool
	TypingDuration    time.Duration
	// Optional delay after sending the message
	DelayAfterSent time.Duration
}

type SendMessageWorker struct {
	cli *whatsmeow.Client
	log zerolog.Logger

	// Ensure only one goroutine sends messages at a time.
	globalQueue chan *MessageJob

	// Queue to just send messages anyway, no throttling, used when
	// IgnoreGlobalQueue is true.
	jobs chan *MessageJob

	// Optional per-receiver queues to avoid flooding a single receiver
	recvQueue         map[types.JID]chan *MessageJob
	recvQueueMu       sync.Mutex
	recvQueueLastUsed map[types.JID]time.Time
}

func NewSendMessageWorker(cli *whatsmeow.Client, log zerolog.Logger) *SendMessageWorker {
	worker := &SendMessageWorker{
		cli:               cli,
		log:               log,
		globalQueue:       make(chan *MessageJob, 20),
		jobs:              make(chan *MessageJob, 20),
		recvQueue:         make(map[types.JID]chan *MessageJob),
		recvQueueLastUsed: make(map[types.JID]time.Time),
	}

	go worker.startWorker(worker.globalQueue)
	for range sendMessageWorkerJobsConcurrency {
		go worker.startWorker(worker.jobs)
	}

	return worker
}

func (w *SendMessageWorker) Enqueue(job *MessageJob) {
	if job.UseReceiverQueue {
		w.log.Debug().Msg("enqueue using receiver queue")
		recvQueue := w.getReceiverQueue(job.To)
		recvQueue <- job
		return
	}

	if job.IgnoreGlobalQueue {
		w.log.Debug().Msg("enqueue using jobs queue")
		w.jobs <- job
		return
	}

	// Use global queue instead
	w.log.Debug().Msg("enqueue using global queue")
	w.globalQueue <- job
}

func (w *SendMessageWorker) Stop() {
	w.log.Debug().Msg("stopping sendMessageWorker")

	close(w.globalQueue)
	for _, queue := range w.recvQueue {
		close(queue)
	}
}

func (w *SendMessageWorker) startWorker(jobs chan *MessageJob) {
	w.log.Debug().Msg("sendMessageWorker started")

	for job := range jobs {
		if job == nil {
			continue
		}

		w.send(job)
	}

	w.log.Debug().Msg("sendMessageWorker exiting")
}

func (w *SendMessageWorker) getReceiverQueue(jid types.JID) chan *MessageJob {
	w.recvQueueMu.Lock()
	defer w.recvQueueMu.Unlock()

	queue, ok := w.recvQueue[jid]
	if ok {
		w.recvQueueLastUsed[jid] = time.Now()
		return queue
	}

	queue = make(chan *MessageJob, sendMessageWorkerQueueSize)
	w.recvQueueLastUsed[jid] = time.Now()
	w.recvQueue[jid] = queue

	w.log.Debug().
		Str("jid", jid.String()).
		Msg("starting receiverWorker")

	go func() {
		for job := range queue {
			if job == nil {
				continue
			}

			w.send(job)
		}

		// channel closed
		w.log.Debug().
			Str("jid", jid.String()).
			Msg("receiverWorker exiting")
	}()

	// Because I don't know when to close, just delay it.
	go func() {
		tick := time.NewTicker(time.Minute)
		for range tick.C {
			if time.Since(w.recvQueueLastUsed[jid]) < recvQueueIdleTimeout {
				continue
			}

			w.recvQueueMu.Lock()
			defer w.recvQueueMu.Unlock()

			w.log.Debug().Msg("closing receiverWorker due to inactivity")

			close(queue)
			delete(w.recvQueue, jid)

			tick.Stop()
			return
		}
	}()

	return queue
}

func (w *SendMessageWorker) send(job *MessageJob) {
	delay := time.Duration(0)
	if job.TypingDuration == 0 {
		w.log.Debug().Msg("delaying using random typing duration")
		delay = sendTypingRand(w.cli, job.To, minDefaultTypingSecond, maxDefaultTypingSecond)
	} else {
		w.log.Debug().Msg("delaying using custom typing duration")
		delay = sendTyping(w.cli, job.To, job.TypingDuration)
	}

	_, err := w.cli.SendMessage(context.TODO(), job.To, job.Message)
	if err != nil {
		w.log.Error().Caller().
			Err(err).
			Msg("unable to send message")
		return
	}

	w.log.Debug().
		Str("jid", w.cli.Store.ID.String()).
		Str("to", job.To.String()).
		Msg("message sent")

	if job.DelayAfterSent < 0 {
		return
	}
	if job.DelayAfterSent == 0 {
		w.log.Debug().Dur("delay", delay).
			Msg("delaying after send with typing duration")

		time.Sleep(delay)
		return
	}

	w.log.Debug().Dur("delay", delay).Msg("delaying after send with custom delay")
	time.Sleep(job.DelayAfterSent)
}
