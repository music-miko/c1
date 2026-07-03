/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package clone

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var tokenRegex = regexp.MustCompile(`\d{8,11}:[A-Za-z0-9_-]{35}`)

// ExtractToken pulls a bot token out of arbitrary text (a command argument
// or a forwarded BotFather message).
func ExtractToken(text string) string {
	return tokenRegex.FindString(text)
}

// BotInfo is the subset of Telegram's getMe response we care about.
type BotInfo struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

// ValidateToken calls Telegram's Bot API getMe endpoint to confirm the
// token is real and to fetch the bot's id/username/name.
func ValidateToken(token string) (*BotInfo, error) {
	if !tokenRegex.MatchString(token) {
		return nil, fmt.Errorf("malformed token")
	}

	httpClient := http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Get(fmt.Sprintf("https://api.telegram.org/bot%s/getMe", token))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out struct {
		OK     bool    `json:"ok"`
		Result BotInfo `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("invalid token")
	}
	return &out.Result, nil
}

// BotIDFromToken extracts the numeric bot ID that prefixes every token
// ("123456789:AAcharacter...").
func BotIDFromToken(token string) (int64, error) {
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("malformed token")
	}
	return strconv.ParseInt(parts[0], 10, 64)
}
