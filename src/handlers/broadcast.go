/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package handlers

import (
	"ashokshau/tgmusic/src/core/db"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	td "github.com/AshokShau/gotdbot"
)

// broadcastState is kept per-bot (keyed by botID) so the main bot and any
// number of clones can each run their own /broadcast independently without
// stepping on each other's cancel/in-progress flags.
type broadcastState struct {
	cancel     atomic.Bool
	inProgress atomic.Bool
}

var (
	broadcastStatesMu sync.Mutex
	broadcastStates   = make(map[int64]*broadcastState)
)

func broadcastStateFor(botID int64) *broadcastState {
	broadcastStatesMu.Lock()
	defer broadcastStatesMu.Unlock()
	s, ok := broadcastStates[botID]
	if !ok {
		s = &broadcastState{}
		broadcastStates[botID] = s
	}
	return s
}

func getFloodWait(err error) int {
	if err == nil {
		return 0
	}

	type retryError interface {
		GetRetryAfter() int
	}

	if re, ok := err.(retryError); ok {
		return re.GetRetryAfter()
	}

	if tdErr, ok := err.(*td.Error); ok {
		return tdErr.GetRetryAfter()
	}

	if tdErr, ok := err.(td.Error); ok {
		return tdErr.GetRetryAfter()
	}

	return 0
}

func cancelBroadcastHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) && !canEdit(c, m) {
		return td.EndGroups
	}

	state := broadcastStateFor(c.Me.Id)
	if !state.inProgress.Load() {
		_, _ = m.ReplyText(c, "No broadcast in progress.", nil)
		return td.EndGroups
	}

	state.cancel.Store(true)
	_, _ = m.ReplyText(c, "Broadcast stopped.", nil)
	return td.EndGroups
}

func broadcastHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		if canEdit(c, m) {
			return cloneOwnerBroadcastHandler(c, m)
		}
		return td.EndGroups
	}

	state := broadcastStateFor(c.Me.Id)
	if state.inProgress.Load() {
		_, _ = m.ReplyText(c, "A broadcast is already in progress.", nil)
		return td.EndGroups
	}

	reply, err := m.GetRepliedMessage(c)
	if err != nil {
		usage := `Please reply to a message to broadcast.

Usage:
-chat  : groups only
-user  : users only
-both  : groups + users (default)
-copy  : send as copy

Examples:
/broadcast
/broadcast -chat
/broadcast -user -copy
`

		_, _ = m.ReplyText(c, usage, nil)
		return td.EndGroups
	}

	args := strings.Fields(Args(m))

	copyMode := false
	mode := "both" // default

	for _, a := range args {
		switch a {
		case "-copy":
			copyMode = true
		case "-chat":
			mode = "chat"
		case "-user":
			mode = "user"
		case "-both":
			mode = "both"
		}
	}

	chats, _ := db.Instance.GetAllChats()
	users, _ := db.Instance.GetAllUsers()

	groupsMap := make(map[int64]bool)
	for _, id := range chats {
		groupsMap[id] = true
	}

	var targets []int64

	switch mode {
	case "chat":
		targets = append(targets, chats...)
	case "user":
		targets = append(targets, users...)
	case "both":
		targets = append(targets, chats...)
		targets = append(targets, users...)
	}

	if len(targets) == 0 {
		_, _ = m.ReplyText(c, "No targets found.", nil)
		return td.EndGroups
	}

	state.cancel.Store(false)
	state.inProgress.Store(true)

	sentMsg, _ := m.ReplyText(c, "Broadcast started.", nil)

	go func() {
		defer state.inProgress.Store(false)

		var failedBuilder strings.Builder
		count, ucount := 0, 0

		for _, chatID := range targets {
			if state.cancel.Load() {
				_, _ = sentMsg.EditText(
					c,
					fmt.Sprintf("Broadcast stopped.\nGroups: %d\nUsers: %d", count, ucount),
					nil,
				)
				return
			}

			var errSend error
			if copyMode {
				_, errSend = reply.Copy(c, chatID, &td.SendCopyOpts{
					ReplyMarkup: reply.ReplyMarkup,
				})
			} else {
				_, errSend = reply.Forward(c, chatID, &td.ForwardMessageOpts{})
			}

			if errSend == nil {
				if groupsMap[chatID] {
					count++
				} else {
					ucount++
				}
				time.Sleep(200 * time.Millisecond)
			} else {
				wait := getFloodWait(errSend)
				if wait > 0 {
					time.Sleep(time.Duration(wait+30) * time.Second)
					continue
				}
				failedBuilder.WriteString(fmt.Sprintf("%d - %v\n", chatID, errSend))
			}
		}

		text := fmt.Sprintf("Broadcast ended.\nGroups: %d\nUsers: %d", count, ucount)
		failedStr := failedBuilder.String()

		if failedStr != "" {
			errFile := filepath.Join(
				os.TempDir(),
				fmt.Sprintf("errors_%d.txt", time.Now().UnixNano()),
			)

			if err := os.WriteFile(errFile, []byte(failedStr), 0644); err == nil {
				defer os.Remove(errFile)

				_, errSendDoc := m.ReplyDocument(
					c,
					td.InputFileLocal{Path: errFile},
					&td.SendDocumentOpts{Caption: text},
				)

				if errSendDoc != nil {
					_, _ = sentMsg.EditText(c, text, nil)
				}
			} else {
				_, _ = sentMsg.EditText(c, text, nil)
			}
		} else {
			_, _ = sentMsg.EditText(c, text, nil)
		}
	}()

	return td.EndGroups
}

// cloneOwnerBroadcastHandler is the scoped /broadcast for a clone owner (or
// clone sudo user): only reaches groups THIS clone is actually in, tracked
// via db.AddCloneChat. DM/user broadcast isn't offered here since clones
// don't keep a separate per-user reachability list (see MIGRATION_GUIDE.md).
func cloneOwnerBroadcastHandler(c *td.Client, m *td.Message) error {
	state := broadcastStateFor(c.Me.Id)
	if state.inProgress.Load() {
		_, _ = m.ReplyText(c, "A broadcast is already in progress.", nil)
		return td.EndGroups
	}

	reply, err := m.GetRepliedMessage(c)
	if err != nil {
		_, _ = m.ReplyText(c, "Please reply to a message to broadcast it to every group this bot is in.\n\nUsage: /broadcast (as a reply)\nAdd -copy to send as a copy instead of a forward.", nil)
		return td.EndGroups
	}

	copyMode := strings.Contains(Args(m), "-copy")

	targets, _ := db.Instance.GetCloneChats(c.Me.Id)
	if len(targets) == 0 {
		_, _ = m.ReplyText(c, "This bot isn't tracked as being in any groups yet.", nil)
		return td.EndGroups
	}

	state.cancel.Store(false)
	state.inProgress.Store(true)
	sentMsg, _ := m.ReplyText(c, fmt.Sprintf("Broadcast started to %d group(s).", len(targets)), nil)

	go func() {
		defer state.inProgress.Store(false)

		count := 0
		for _, chatID := range targets {
			if state.cancel.Load() {
				_, _ = sentMsg.EditText(c, fmt.Sprintf("Broadcast stopped.\nGroups: %d", count), nil)
				return
			}

			var errSend error
			if copyMode {
				_, errSend = reply.Copy(c, chatID, &td.SendCopyOpts{ReplyMarkup: reply.ReplyMarkup})
			} else {
				_, errSend = reply.Forward(c, chatID, &td.ForwardMessageOpts{})
			}

			if errSend == nil {
				count++
				time.Sleep(200 * time.Millisecond)
			} else if wait := getFloodWait(errSend); wait > 0 {
				time.Sleep(time.Duration(wait+30) * time.Second)
			}
		}

		_, _ = sentMsg.EditText(c, fmt.Sprintf("Broadcast ended.\nGroups: %d", count), nil)
	}()

	return td.EndGroups
}
