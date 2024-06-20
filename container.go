package main

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/9d4/wadoh-be/pb"
	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

var (
	ErrDeviceNotFound  = errors.New("device not found")
	ErrWebhookNotFound = errors.New("webhook not found")
)

type Container struct {
	db *sql.DB
	*sqlstore.Container
	logger zerolog.Logger
}

func NewContainer(dbc *DBConfig, logger zerolog.Logger) (*Container, error) {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_foreign_keys=on", dbc.DBPath))
	if err != nil {
		return nil, fmt.Errorf("container: %w", err)
	}
	container := sqlstore.NewWithDB(db, "sqlite3", waLog.Zerolog(logger))
	err = container.Upgrade()
	if err != nil {
		return nil, err
	}

	m := &Container{
		db:        db,
		Container: container,
		logger:    logger,
	}

	// upgrade custom migrations
	if err := m.upgradeAll(); err != nil {
		return nil, err
	}

	return m, nil
}

// NewClient creates new Client only if device already stored.
// NewClient will return error ErrDeviceNotFound if device not found.
func (c *Container) NewClient(id string) (*whatsmeow.Client, error) {
	jid, err := types.ParseJID(id)
	if err != nil {
		return nil, err
	}

	device, err := c.GetDevice(jid)
	if err != nil {
		return nil, err
	}

	if device == nil {
		return nil, ErrDeviceNotFound
	}

	logger := c.logger.With().Str("logger", "client:"+jid.String()).Logger()
	cli := whatsmeow.NewClient(device, waLog.Zerolog(logger))

	return cli, err
}

func (c *Container) getWebhook(jid string) (*pb.GetWebhookResponse, error) {
	const query = "SELECT `url`, `timestamp` FROM `wadohbe_webhooks` " +
		"WHERE `jid` = ? LIMIT 1"
	row := c.db.QueryRow(query, jid)
	if row.Err() != nil {
		return nil, row.Err()
	}
	res := &pb.GetWebhookResponse{}
	if err := row.Scan(&res.Url, &res.Timestamp); err != nil {
		return nil, err
	}
	return res, nil
}

func (c *Container) saveWebhook(jid, url string) error {
	const query = "INSERT INTO `wadohbe_webhooks` (`jid`, `url`, `timestamp`)" +
		"VALUES (?, ?, ?)"

	if err := c.deleteWebhook(jid); err != nil {
		return err
	}
	_, err := c.db.Exec(query, jid, url, time.Now().UnixMilli())
	if err != nil {
		return err
	}
	return nil
}

func (c *Container) deleteWebhook(jid string) error {
	const query = "DELETE FROM `wadohbe_webhooks` WHERE `jid` = ?"
	_, err := c.db.Exec(query, jid)
	if err != nil {
		return err
	}
	return nil
}
