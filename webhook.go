package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

func startWebhook(c *Controller, ec chan *EventMessage, max int) {
	wg := sync.WaitGroup{}
	wg.Add(max)
	for i := 0; i < max; i++ {
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
