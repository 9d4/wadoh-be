package main

import (
	"errors"
	"fmt"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type Container struct {
	*sqlstore.Container
	logger zerolog.Logger
}

func NewContainer(dbc *DBConfig, logger zerolog.Logger) (*Container, error) {
	container, err := sqlstore.New("sqlite3", fmt.Sprintf("file:%s?_foreign_keys=on", dbc.DBPath), waLog.Zerolog(logger))
	if err != nil {
		return nil, err
	}

	m := &Container{
		Container: container,
		logger:    logger,
	}
	return m, nil
}

// NewClient creates new Client only if device already stored.
// NewClient will return error ErrDeviceNotFound if device not found.
func (cont *Container) NewClient(id string) (*whatsmeow.Client, error) {
	jid, err := types.ParseJID(id)
	if err != nil {
		return nil, err
	}

	device, err := cont.GetDevice(jid)
	if err != nil {
		return nil, err
	}

	if device == nil {
		return nil, ErrDeviceNotFound
	}

	logger := cont.logger.With().Str("logger", "client:"+jid.String()).Logger()
	cli := whatsmeow.NewClient(device, waLog.Zerolog(logger))

	return cli, err
}

var (
	ErrDeviceNotFound = errors.New("device not found")
)
