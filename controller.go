package main

import (
	"context"
	"errors"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/9d4/wadoh/pb"
)

type Controller struct {
	container *Container
	logger    zerolog.Logger

	clients     map[string]*whatsmeow.Client
	clientsLock sync.Mutex

	recvMessageC     []chan *EventMessage
	recvMessageCLock sync.Mutex
}

func NewController(container *Container, logger zerolog.Logger) *Controller {
	c := &Controller{
		container:    container,
		logger:       logger,
		clients:      make(map[string]*whatsmeow.Client),
		recvMessageC: make([]chan *EventMessage, 0),
	}
	return c
}

func (c *Controller) loop() {
	c.ConnectAllDevices()
	tick := time.NewTicker(10 * time.Second)
	for {
		c.logger.Debug().Msg("loop start")

		c.logger.Debug().Msg("loop end")
		<-tick.C
	}
}

func (c *Controller) ConnectAllDevices() error {
	devices, err := c.container.GetAllDevices()
	if err != nil {
		return err
	}

	for _, d := range devices {
		d := d
		go c.connectDevice(d)
	}

	return nil
}

func (c *Controller) getClient(jid string) (*whatsmeow.Client, error) {
	c.clientsLock.Lock()
	if cli, ok := c.clients[jid]; ok {
		c.clientsLock.Unlock()
		return cli, nil
	}
	c.clientsLock.Unlock()

	cli, err := c.container.NewClient(jid)
	if err != nil {
		return nil, err
	}

	c.clientsLock.Lock()
	c.clients[jid] = cli
	c.clientsLock.Unlock()

	return cli, err
}

func (c *Controller) connectDevice(device *store.Device) {
	cli, err := c.getClient(device.ID.String())
	if err != nil {
		return
	}

	if cli.IsConnected() {
		return
	}

	err = cli.Connect()
	if err != nil {
		c.logger.Err(err).Str("id", device.ID.String()).Msg("unable to connect client")
		return
	}

	cli.AddEventHandler(c.eventHandler(cli.Store.ID.String()))
}

func (c *Controller) eventHandler(jid string) func(interface{}) {
	send := func(evt *EventMessage) {
		for _, ch := range c.recvMessageC {
			ch <- evt
		}
	}

	fn := func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Message:
			event := &EventMessage{
				JID:     jid,
				From:    v.Info.Sender.User,
				Message: v.Message.GetConversation(),
			}
			send(event)
			c.logger.Debug().Any("evtMessage", v).Msg("sent message event to channels")

		default:
			c.logger.Debug().Msgf("unhandled event case: %#+v", evt)
		}
	}

	return fn
}
func (c *Controller) Status(jid string) (pb.StatusResponse_Status, error) {
	cli, err := c.getClient(jid)
	if err != nil {
		if errors.Is(err, ErrDeviceNotFound) {
			return pb.StatusResponse_STATUS_NOT_FOUND, nil
		}

		return pb.StatusResponse_STATUS_UNKNOWN, err
	}

	if cli.IsConnected() && cli.IsLoggedIn() {
		return pb.StatusResponse_STATUS_ACTIVE, nil
	}
	if !cli.IsConnected() {
		return pb.StatusResponse_STATUS_DISCONNECTED, nil
	}

	return pb.StatusResponse_STATUS_UNKNOWN, nil
}

// RegisterNewDevice requests new device registration
func (c *Controller) RegisterNewDevice(
	req *pb.RegisterDeviceRequest,
	resc chan *pb.RegisterDeviceResponse,
	done chan struct{},
) error {
	device, err := c.container.GetDevice(types.NewJID(req.Phone, types.DefaultUserServer))
	if err != nil {
		return err
	}
	if device == nil {
		device = c.container.NewDevice()
	}

	logger := c.container.logger.With().Str("logger", "client-reg:"+req.Phone).Logger()
	cli := whatsmeow.NewClient(device, waLog.Zerolog(logger))
	if cli.Store.ID != nil {
		return errors.New("device already registered")
	}

	qrChan, err := cli.GetQRChannel(context.Background())
	if err != nil {
		return err
	}
	if err := cli.Connect(); err != nil {
		return err
	}

	pairSuccess := false
	go func() {
		<-done
		c.logger.Debug().Msg("cleanup on <-done closed")
		defer c.logger.Debug().Msg("cleanup done")
		if !pairSuccess {
			// this condition indicates the caller don't need response anymore (network error or client disconnected).
			// or qrChan has ran out.
			c.logger.Debug().Msg("disconnecting due to cli is not loggedIn")
			cli.Disconnect()
			cli.RemoveEventHandlers()
		}
	}()

	go func() {
		defer c.logger.Debug().Msg("qrchan routine exited")
		for {
			select {
			case <-done:
				return
			case item := <-qrChan:
				if item.Event == "" {
					// qrChan is closed
					close(done)
					return
				}

				if item.Event == whatsmeow.QRChannelEventCode {
					c.logger.Info().Str("code", item.Code).Send()
					select {
					case <-done:
						return
					default:
						resc <- &pb.RegisterDeviceResponse{Qr: proto.String(item.Code)}
						continue
					}
				}

				if item.Event == "success" {
					select {
					case <-done:
						return
					default:
						pairSuccess = true
						resc <- &pb.RegisterDeviceResponse{
							Jid:      proto.String(cli.Store.ID.String()),
							LoggedIn: proto.Bool(true),
						}

						// add to cache
						c.clientsLock.Lock()
						defer c.clientsLock.Unlock()
						c.clients[cli.Store.ID.String()] = cli

						close(done)
						return
					}

				}
			}
		}
	}()

	go func() {
		defer c.logger.Debug().Msg("paircode routine exited")
		tick := time.NewTicker(3 * time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			default:
				code, err := cli.PairPhone(req.Phone, req.PushNotification, whatsmeow.PairClientChrome, "Chrome (MacOS)")
				if err == nil {
					resc <- &pb.RegisterDeviceResponse{PairCode: proto.String(code)}
					c.logger.Debug().Msg("paircode sent to channel")
				}

				select {
				case <-done:
					return
				case <-tick.C:
				}
			}
		}
	}()

	return nil
}

func (c *Controller) SendMessage(ctx context.Context, req *pb.SendMessageRequest) error {
	cli, err := c.getClient(req.Jid)
	if err != nil {
		return err
	}
	toJid := types.NewJID(req.Phone, types.DefaultUserServer)

	go func(body string) {
		const min, max = 1, 4
		delay := time.Duration(rand.IntN(max-min)+max) * time.Second

		cli.SendPresence(types.PresenceAvailable)
		cli.SendChatPresence(toJid, types.ChatPresenceComposing, types.ChatPresenceMediaText)

		tick := time.NewTicker(delay)
		defer tick.Stop()
		c.logger.Debug().Dur("typing delay", delay).Send()
		<-tick.C

		cli.SendChatPresence(toJid, types.ChatPresencePaused, types.ChatPresenceMediaText)
		cli.SendPresence(types.PresenceUnavailable)

		if _, err := cli.SendMessage(context.Background(), toJid, &waProto.Message{
			Conversation: &body,
		}); err != nil {
			c.logger.Debug().Caller().Err(err).Send()
		} else {
			c.logger.Debug().Str("jid", req.Jid).Str("to", toJid.String()).Msg("message sent")
		}
	}(req.Body)

	return nil
}

func (c *Controller) ReceiveMessage(ctx context.Context) (<-chan *EventMessage, error) {
	ch := make(chan *EventMessage)

	c.recvMessageCLock.Lock()
	defer c.recvMessageCLock.Unlock()
	c.recvMessageC = append(c.recvMessageC, ch)

	go func() {
		<-ctx.Done()
		c.logger.Debug().Msg("cleaning recv message channel")
		c.recvMessageCLock.Lock()
		defer c.recvMessageCLock.Unlock()

		c.recvMessageC = slices.DeleteFunc(c.recvMessageC, func(c chan *EventMessage) bool {
			return c == ch
		})

		c.logger.Debug().Int("count", len(c.recvMessageC)).Msg("recvMessageC")
	}()

	return ch, nil
}

type EventMessage struct {
	JID     string
	From    string
	Message string
}
