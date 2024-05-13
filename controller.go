package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/9d4/wadoh/pb"
)

type Controller struct {
	container *Container
	logger    zerolog.Logger

	clients     map[string]*whatsmeow.Client
	clientsLock sync.Mutex
}

func NewController(container *Container, logger zerolog.Logger) *Controller {
	c := &Controller{
		container: container,
		logger:    logger,
		clients:   make(map[string]*whatsmeow.Client),
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
func (c *Controller) RegisterNewDevice(ctx context.Context, req *pb.RegisterDeviceRequest, qr, pairCode, jid chan string) (<-chan struct{}, error) {
	device, err := c.container.GetDevice(types.NewJID(req.Phone, types.DefaultUserServer))
	if err != nil {
		return nil, err
	}

	if device == nil {
		device = c.container.NewDevice()
	}

	logger := c.container.logger.With().Str("logger", "client-reg:"+req.Phone).Logger()
	cli := whatsmeow.NewClient(device, waLog.Zerolog(logger))

	if cli.Store.ID != nil {
		return nil, errors.New("device already registered")
	}

	qrChan, err := cli.GetQRChannel(context.Background())
	if err != nil {
		return nil, err
	}

	if err := cli.Connect(); err != nil {
		return nil, err
	}

	done := make(chan struct{})

	go func() {
		<-ctx.Done()
		cli.Disconnect()
		cli.RemoveEventHandlers()
	}()

	go func() {
		defer c.logger.Debug().Msg("qrchan routine exited")
		for {
			select {
			case <-ctx.Done():
				close(done)
				return
			case item := <-qrChan:
				c.logger.Info().Str("code", item.Code).Send()
				if item.Event == whatsmeow.QRChannelEventCode {
					select {
					case <-ctx.Done():
						return
					default:
						qr <- item.Code
						continue
					}
				}

				if item.Event == "success" {
					select {
					case <-ctx.Done():
						return
					default:
						jid <- cli.Store.ID.String()
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

		// qrChan is closed, exit now
		close(done)
	}()

	go func() {
		defer c.logger.Debug().Msg("paircode routine exited")
		tick := time.NewTicker(3 * time.Minute)
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			default:
			}

			code, err := cli.PairPhone(req.Phone, req.PushNotification, whatsmeow.PairClientChrome, "Chrome (MacOS)")
			if err == nil {
				pairCode <- code
				c.logger.Debug().Msg("paircode sent to channel")
			}

			<-tick.C
		}
	}()

	return done, nil
}
