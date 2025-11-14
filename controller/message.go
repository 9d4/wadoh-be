package controller

import (
	"context"
	"math/rand/v2"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

func sendTyping(cli *whatsmeow.Client, toJid types.JID, dur time.Duration) time.Duration {
	if dur <= 0 {
		return 0
	}

	cli.SendChatPresence(context.TODO(), toJid, types.ChatPresenceComposing, types.ChatPresenceMediaText)
	time.Sleep(dur)
	cli.SendChatPresence(context.TODO(), toJid, types.ChatPresencePaused, types.ChatPresenceMediaText)
	return dur
}

func sendTypingRand(cli *whatsmeow.Client, toJid types.JID, minSec, maxSec int) time.Duration {
	dur := minSec
	if maxSec > minSec {
		r := rand.IntN(maxSec-minSec+1) + minSec
		dur = r
	}
	d := time.Duration(dur) * time.Second
	sendTyping(cli, toJid, d)
	return d
}
