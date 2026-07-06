/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package vc

import (
	"ashokshau/tgmusic/config"
	"ashokshau/tgmusic/src/core/db"
	"ashokshau/tgmusic/src/utils"
	"fmt"

	td "github.com/AshokShau/gotdbot"
)

// sendLogger sends a formatted log message to the designated logger chat.
// It includes details about the song being played, such as its title, duration, and the user who requested it.
func sendLogger(client *td.Client, chatID int64, song *utils.CachedTrack) {
	if chatID == 0 || song == nil {
		return
	}

	loggerID := config.LoggerId
	if reg, _ := db.Instance.GetClone(client.Me.Id); reg != nil {
		// This is a clone bot — it isn't a member of the main bot's log
		// channel, so sending there always fails. Use its own configured
		// logger, or skip entirely if the clone owner hasn't set one up.
		if !reg.LoggerEnabled || reg.LoggerID == 0 {
			return
		}
		loggerID = reg.LoggerID
	}
	if chatID == loggerID {
		return
	}

	text := fmt.Sprintf(
		"<b>A song is playing</b> in <code>%d</code>\n\n‣ <b>Title:</b> <a href='%s'>%s</a>\n‣ <b>Duration:</b> %s\n‣ <b>Requested by:</b> %s\n‣ <b>Platform:</b> %s\n‣ <b>Is Video:</b> %t",
		chatID,
		song.URL,
		song.Name,
		utils.SecToMin(song.Duration),
		song.User,
		song.Platform,
		song.IsVideo,
	)

	_, err := client.SendTextMessage(loggerID, text, &td.SendTextMessageOpts{DisableWebPagePreview: true, ParseMode: "HTML"})
	if err != nil {
		logger.Warn("Failed to send the message", "error", err)
	}
}
