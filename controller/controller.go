package controller

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/9d4/wadoh-be/container"
	"github.com/9d4/wadoh-be/pb"
)

var maxWebhookWorker = 100

var (
	ErrDeviceNotFound  = errors.New("device not found")
	ErrWebhookNotFound = errors.New("webhook not found")
)

type Controller struct {
	container *container.Container
	logger    zerolog.Logger

	clients     map[string]*whatsmeow.Client
	clientsLock sync.Mutex

	// recvMessageC contains channels for event subscribers
	recvMessageC     []chan *EventMessage
	recvMessageCLock sync.Mutex

	sendMessageLocks *SafeMap
}

func NewController(container *container.Container, logger zerolog.Logger) *Controller {
	c := &Controller{
		container:        container,
		logger:           logger,
		clients:          make(map[string]*whatsmeow.Client),
		recvMessageC:     make([]chan *EventMessage, 0),
		sendMessageLocks: NewSafeMap(),
	}

	workerChan := make(chan *EventMessage)
	c.recvMessageC = append(c.recvMessageC, workerChan)
	c.startWebhookWorker(workerChan)

	return c
}

func (c *Controller) Start() {
	c.ConnectAllDevices()
	tick := time.NewTicker(10 * time.Second)
	for {
		c.logger.Trace().Msg("loop start")
		c.ConnectAllDevices()
		c.logger.Trace().Msg("loop end")
		<-tick.C
	}
}

func (c *Controller) startWebhookWorker(ec chan *EventMessage) {
	startWebhook(c, ec, maxWebhookWorker)
}

func startWebhook(c *Controller, ec chan *EventMessage, max int) {
	wg := sync.WaitGroup{}
	wg.Add(max)
	for range max {
		go webhookWorker(c, ec)
		wg.Done()
	}
	wg.Wait()
}

func webhookWorker(c *Controller, ec chan *EventMessage) {
	cli := &http.Client{
		Timeout: 10 * time.Second,
	}

	for ev := range ec {
		if ev == nil {
			continue
		}

		res, err := c.GetWebhook(context.Background(), ev.JID)
		if err != nil {
			log.Debug().Caller().Err(err)
			continue
		}

		var body bytes.Buffer
		if err := json.NewEncoder(&body).Encode(ev); err != nil {
			log.Debug().Caller().Err(fmt.Errorf("Error encoding json: %w", err))
			continue
		}

		req, err := http.NewRequest("POST", res.GetUrl(), &body) // Assuming it's a POST request
		if err != nil {
			log.Debug().Caller().Err(fmt.Errorf("Error creating request: %w", err))
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := cli.Do(req)
		if err != nil {
			log.Debug().Caller().Err(fmt.Errorf("Error executing request: %w", err))
			continue
		}

		if resp.StatusCode != http.StatusOK {
			log.Debug().Caller().Err(fmt.Errorf("Unexpected response status: %v", resp.Status))
			continue
		}
	}
}

func (c *Controller) Shutdown() <-chan error {
	finish := make(chan error)
	go func() {
		var wg sync.WaitGroup
		wg.Add(len(c.clients))
		wg.Add(len(c.recvMessageC))

		for _, cli := range c.clients {
			cli.Disconnect()
			wg.Done()
		}
		for _, ec := range c.recvMessageC {
			close(ec)
			wg.Done()
		}
		wg.Wait()
		finish <- nil
	}()

	return finish
}

func (c *Controller) ConnectAllDevices() error {
	devices, err := c.container.GetAllDevices(context.Background())
	if err != nil {
		return err
	}

	knownIDs := []string{}

	for _, d := range devices {
		d := d
		go c.connectDevice(d)

		// Check if the device does not have message lock mutex then create it.
		if mu := c.sendMessageLocks.GetMutex(d.ID.String()); mu == nil {
			c.sendMessageLocks.AddMutex(d.ID.String())
		}

		knownIDs = append(knownIDs, d.ID.String())
	}

	c.sendMessageLocks.DeleteUnknownKeys(knownIDs)
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

func (c *Controller) eventHandler(jid string) func(any) {
	send := func(evt *EventMessage) {
		for _, ch := range c.recvMessageC {
			ch <- evt
		}
	}

	fn := func(evt any) {
		switch v := evt.(type) {
		case *events.Message:
			event := &EventMessage{
				JID:     jid,
				From:    v.Info.Sender.User,
				Message: v.Message.ExtendedTextMessage.GetText(),
			}

			// TODO: better handling message and media
			// for now just handle text, if no nothing in it don't send event
			if event.Message == "" {
				return
			}

			send(event)
			c.logger.Info().Any("evtMessage", event).Msg("sent message event to channels")
		}
	}

	return fn
}

func (c *Controller) Status(jid string) (pb.StatusResponse_Status, error) {
	cli, err := c.getClient(jid)
	if err != nil {
		if errors.Is(err, ErrDeviceNotFound) || errors.Is(err, os.ErrNotExist) {
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
	device, err := c.container.GetDevice(context.Background(), types.NewJID(req.Phone, types.DefaultUserServer))
	if err != nil {
		return err
	}
	if device == nil {
		device = c.container.NewDevice()
	}

	logger := c.logger.With().Str("logger", "client-reg:"+req.Phone).Logger()
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
				code, err := cli.PairPhone(context.Background(), req.Phone, req.PushNotification, whatsmeow.PairClientChrome, "Chrome (MacOS)")
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

	mu := c.sendMessageLocks.GetMutex(cli.Store.ID.String())
	if mu == nil {
		return fmt.Errorf("mutex not found: %w", ErrDeviceNotFound)
	}

	go func(body string) {
		mu.Lock()
		defer mu.Unlock()

		const min, max = 1, 4
		delay := time.Duration(rand.IntN(max-min)+max) * time.Second

		cli.SendPresence(context.TODO(), types.PresenceAvailable)
		cli.SendChatPresence(context.TODO(), toJid, types.ChatPresenceComposing, types.ChatPresenceMediaText)

		c.logger.Debug().Dur("typing delay", delay).Send()
		time.Sleep(delay)

		cli.SendChatPresence(context.TODO(), toJid, types.ChatPresencePaused, types.ChatPresenceMediaText)
		cli.SendPresence(context.TODO(), types.PresenceUnavailable)

		if _, err := cli.SendMessage(context.TODO(), toJid, &waE2E.Message{
			Conversation: &body,
		}); err != nil {
			c.logger.Error().Caller().Err(err).Msg("unable to send message")
		} else {
			c.logger.Debug().Str("jid", req.Jid).Str("to", toJid.String()).Msg("message sent")
		}

		// add delay before next message
		time.Sleep(delay)
	}(req.Body)

	return nil
}

func (c *Controller) SendMessageImage(ctx context.Context, req *pb.SendImageMessageRequest, mime string) error {
	cli, err := c.getClient(req.Jid)
	if err != nil {
		return err
	}
	mu := c.sendMessageLocks.GetMutex(cli.Store.ID.String())
	if mu == nil {
		return fmt.Errorf("mutex not found: %w", ErrDeviceNotFound)
	}
	toJid := types.NewJID(req.Phone, types.DefaultUserServer)

	go func() {
		mu.Lock()
		defer mu.Unlock()

		resp, err := cli.Upload(context.TODO(), req.Image, whatsmeow.MediaImage)
		if err != nil {
			log.Error().Caller().
				Err(err).
				Str("jid", req.Jid).
				Str("to", toJid.String()).
				Msg("unable to upload image")
			return
		}

		msg := &waE2E.Message{
			ImageMessage: &waE2E.ImageMessage{
				Caption:       proto.String(req.Caption),
				Mimetype:      proto.String(mime),
				URL:           &resp.URL,
				DirectPath:    &resp.DirectPath,
				MediaKey:      resp.MediaKey,
				FileEncSHA256: resp.FileEncSHA256,
				FileSHA256:    resp.FileSHA256,
				FileLength:    &resp.FileLength,
			},
		}

		sendPresenceTyping(cli, toJid)
		if _, err := cli.SendMessage(context.TODO(), toJid, msg); err != nil {
			log.Error().Caller().
				Err(err).
				Str("jid", req.Jid).
				Str("to", toJid.String()).
				Msg("unable to send image message")
			return
		}

		log.Debug().Str("jid", req.Jid).Str("to", toJid.String()).Msg("image message sent")
	}()

	return nil
}

func sendPresenceTyping(cli *whatsmeow.Client, toJid types.JID) {
	cli.SendChatPresence(context.TODO(), toJid, types.ChatPresenceComposing, types.ChatPresenceMediaText)
	const min, max = 1, 4
	delay := time.Duration(rand.IntN(max-min)+max) * time.Second
	time.Sleep(delay)
	cli.SendChatPresence(context.TODO(), toJid, types.ChatPresencePaused, types.ChatPresenceMediaText)
}

type EventMessage struct {
	JID     string `json:"jid"`
	From    string `json:"from"`
	Message string `json:"message"`
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

func (c *Controller) GetWebhook(ctx context.Context, jid string) (*pb.GetWebhookResponse, error) {
	res, err := c.container.GetWebhook(jid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWebhookNotFound
	}
	return res, err
}

func (c *Controller) SaveWebhook(ctx context.Context, jid, url string) error {
	if _, err := c.getClient(jid); err != nil {
		return ErrDeviceNotFound
	}
	if err := c.container.SaveWebhook(jid, url); err != nil {
		return err
	}

	return nil
}

func (c *Controller) DeleteWebhook(ctx context.Context, jid string) error {
	if err := c.container.DeleteWebhook(jid); err != nil {
		return err
	}
	return nil
}
