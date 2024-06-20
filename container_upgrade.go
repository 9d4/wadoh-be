// Wadoh BE may have custom table and migrations whitin same database as
// whatsmeow.
// This file is where to define migrations. Every table should use prefix
// `wadohbe` to prevent conflict.
package main

import (
	"database/sql"
	"fmt"

	"github.com/rs/zerolog/log"
)

const (
	tablePrefix = "wadohbe"
)

var versions = []func(*sql.Tx, *Container) error{upgradeV1}

func (c *Container) upgradeAll() error {
	version, err := c.getVersion()
	if err != nil {
		return err
	}

	for ; version < len(versions); version++ {
		target := version + 1
		tx, err := c.db.Begin()
		if err != nil {
			return err
		}
		fn := versions[version]
		if err := fn(tx, c); err != nil {
			tx.Rollback()
			return fmt.Errorf("upgrade tx %d: %w", target, err)
		}

		if err := setVersion(tx, target); err != nil {
			tx.Rollback()
			return fmt.Errorf("upgrade setVersion %d: %w", target, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}

		log.Info().Msgf("wadohbe upgraded to version %d", target)
	}

	return nil
}

func (c *Container) getVersion() (int, error) {
	_, err := c.db.Exec("CREATE TABLE IF NOT EXISTS wadohbe_version (version INTEGER)")
	if err != nil {
		return -1, err
	}

	version := 0
	row := c.db.QueryRow("SELECT version FROM wadohbe_version LIMIT 1")
	if row != nil {
		_ = row.Scan(&version)
	}
	return version, nil
}

func setVersion(tx *sql.Tx, v int) error {
	_, err := tx.Exec("DELETE FROM wadohbe_version")
	if err != nil {
		return err
	}

	_, err = tx.Exec("INSERT INTO wadohbe_version (version) VALUES (?)", v)
	if err != nil {
		return err
	}
	return nil
}

func upgradeV1(tx *sql.Tx, c *Container) error {
	_, err := tx.Exec(`CREATE TABLE wadohbe_webhooks (
        jid TEXT NOT NULL,
        url TEXT NOT NULL,
        timestamp BIGINT NOT NULL,
        PRIMARY KEY (jid),
        FOREIGN KEY (jid) REFERENCES whatsmeow_device(jid) ON DELETE CASCADE ON UPDATE CASCADE
    )`)
	if err != nil {
		return err
	}
	return nil
}
