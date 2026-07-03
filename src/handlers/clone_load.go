/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package handlers

import (
	"log/slog"

	"github.com/AshokShau/gotdbot"
	"github.com/AshokShau/gotdbot/filters/callbackquery"
	"github.com/AshokShau/gotdbot/filters/message"
)

// LoadCloneModules registers the /clone family of commands. Call this ONLY
// on the primary bot client (never on a clone client), mirroring how
// clone.py in the Python bot is a plugin of the main app, not of each
// spawned clone.
func LoadCloneModules(c *gotdbot.Client) {
	c.OnCommand("clone", cloneCmdHandler)
	c.OnCommand("mybot", myCloneCmdHandler)
	c.OnCommand("myclone", myCloneCmdHandler)
	c.OnCommand("deleteclone", deleteCloneCmdHandler)
	c.OnCommand("removeclone", deleteCloneCmdHandler)
	c.OnCommand("stopclone", stopCloneCmdHandler)
	c.OnCommand("cstats", cstatsCmdHandler)

	c.OnUpdateNewCallbackQuery(cloneCallbackHandler, callbackquery.Prefix("clone_"))

	// Forwarded BotFather message catch-all: any forwarded message in a
	// private chat gets scanned for a bot token.
	c.OnUpdateNewMessage(func(client *gotdbot.Client, u *gotdbot.UpdateNewMessage) error {
		return forwardedTokenHandler(client, u.Message)
	}, gotdbot.NewUpdateNewMessageFilter(message.And(
		message.Private,
		func(msg *gotdbot.Message) bool {
			return msg.ForwardInfo != nil
		},
	)))

	slog.Debug("[clone] primary-bot clone commands loaded")
}

// LoadCloneOwnerModules registers /edit and its supporting handlers. Call
// this ONLY on clone clients (never on the primary bot) — a clone owner
// configures their bot by DM'ing that bot directly, mirroring
// anony/clone/plugins/edit.py.
func LoadCloneOwnerModules(c *gotdbot.Client) {
	c.OnCommand("edit", editCmdHandler)
	c.OnUpdateNewCallbackQuery(editCallbackHandler, callbackquery.Prefix("cedit_"))

	c.OnUpdateNewMessage(func(client *gotdbot.Client, u *gotdbot.UpdateNewMessage) error {
		return editInputHandler(client, u.Message)
	}, gotdbot.NewUpdateNewMessageFilter(message.And(
		message.Private,
		message.Not(message.Command),
	)))

	slog.Debug("[clone] clone-owner /edit commands loaded")
}
