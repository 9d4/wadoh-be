package controller

import (
	"reflect"
	"sync"
	"time"

	"github.com/jellydator/ttlcache/v3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type ClientEventHandler struct {
	JID               string
	logger            zerolog.Logger
	lastSentMessageID *ttlcache.Cache[string, struct{}]

	// Making sure handle event once at a time
	mu sync.Mutex

	onMessage func(event *EventMessage)
}

func NewClientEventHandler(jid string, logger zerolog.Logger) *ClientEventHandler {
	return &ClientEventHandler{
		JID: jid,
		lastSentMessageID: ttlcache.New(
			ttlcache.WithTTL[string, struct{}](30 * time.Second),
		),
		logger: logger.With().
			Str("subLogger", "ClientEventHandler").
			Str("jid", jid).
			Logger(),
	}
}

func (h *ClientEventHandler) callOnMessage(evt *EventMessage) {
	if h.onMessage != nil {
		h.onMessage(evt)
	}
}

func (h *ClientEventHandler) handleEvent(evt any) {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch v := evt.(type) {
	case *events.Message:
		h.logger.Debug().
			Str("event", "message").
			Any("raw", v).
			Msg("message event")

		msg := v.Message.ExtendedTextMessage.GetText()
		if msg == "" {
			msg = v.Message.GetConversation()
		}

		if msg == "" {
			h.logger.Debug().Msg("ignore empty message")
			return
		}

		if v.Info.IsFromMe {
			h.logger.Debug().Msg("ignore message from self")
			return
		}
		if v.Info.IsGroup {
			h.logger.Debug().Msg("ignore message from group")
			return
		}

		sender := v.Info.Sender
		from := ""
		if sender.Server == types.DefaultUserServer {
			from = sender.User
		} else {
			alt := v.Info.SenderAlt
			if alt.Server != types.DefaultUserServer {
				h.logger.Warn().
					Str("sender", sender.String()).
					Str("alt", alt.String()).
					Msg("unable to determine sender phone number from non-default server JID")
				return
			}
			from = alt.User

		}

		if h.lastSentMessageID.Has(v.Info.ID) {
			log.Error().Msg("ignore message that was just sent by us")
			return
		}

		log.Info().Str("event", "message").
			Str("jid", h.JID).
			Str("from", from).
			Str("message_id", v.Info.ID).
			Str("message", msg).
			Msg("received message, and triggering onMessage callback")

		event := &EventMessage{
			JID:       h.JID,
			From:      from,
			MessageID: v.Info.ID,
			Message:   msg,
		}

		h.callOnMessage(event)
		h.lastSentMessageID.Set(v.Info.ID, struct{}{}, 0)

	default:
		// Get type with reflection
		t := reflect.TypeOf(v)
		h.logger.Info().
			Str("event", t.Name()).
			Any("raw", v).
			Msg("unhandled event type")

	}
}
