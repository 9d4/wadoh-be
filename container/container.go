package container

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/9d4/wadoh-be/pb"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type Container struct {
	db *sql.DB
	*sqlstore.Container
	logger zerolog.Logger
}

func NewContainer(dbpath string, logger zerolog.Logger) (*Container, error) {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_foreign_keys=on", dbpath))
	if err != nil {
		return nil, fmt.Errorf("container: %w", err)
	}
	container := sqlstore.NewWithDB(db, "sqlite3", waLog.Zerolog(logger))
	err = container.Upgrade(context.Background())
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

	device, err := c.GetDevice(context.Background(), jid)
	if err != nil {
		return nil, err
	}

	if device == nil {
		return nil, os.ErrNotExist
	}

	logger := c.logger.With().Str("clientLogger", "client:"+jid.String()).Logger()
	cli := whatsmeow.NewClient(device, waLog.Zerolog(logger))

	return cli, err
}

func (c *Container) GetWebhook(jid string) (*pb.GetWebhookResponse, error) {
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

func (c *Container) SaveWebhook(jid, url string) error {
	const query = "INSERT INTO `wadohbe_webhooks` (`jid`, `url`, `timestamp`)" +
		"VALUES (?, ?, ?)"

	if err := c.DeleteWebhook(jid); err != nil {
		return err
	}
	_, err := c.db.Exec(query, jid, url, time.Now().UnixMilli())
	if err != nil {
		return err
	}
	return nil
}

func (c *Container) DeleteWebhook(jid string) error {
	const query = "DELETE FROM `wadohbe_webhooks` WHERE `jid` = ?"
	_, err := c.db.Exec(query, jid)
	if err != nil {
		return err
	}
	return nil
}
