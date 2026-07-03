/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 *
 * This file adds bot-aware assistant assignment on top of the existing
 * single-key (chatID -> assistant) model in calls.go / userbot.go / play.go.
 *
 * Why this is needed: an *Assistant wraps one ntgcalls binding instance tied
 * to one userbot account. Assistant.Play() keys its internal call table by
 * chatID alone (see assistant.go: `a.binding.Calls()[chatId]`), so if two
 * different bots were handed the SAME assistant for the SAME chat, the
 * second Play() would silently replace the first bot's stream instead of
 * starting an independent one. The fix, exactly like the Python
 * CloneAssistantManager this mirrors, is to hand out a DIFFERENT assistant
 * per (botID, chatID) pair whenever there's a collision, and to report
 * "all assistants busy" once the dedicated pool for that chat is exhausted.
 *
 * The primary bot keeps using the existing getClientIndex/GetGroupAssistant
 * path untouched (via PlayMedia/Stop/Pause/... — nothing here changes their
 * behaviour). Everything below is purely additive: new *For methods that
 * handlers call with their own bot's ID so multiple bots can coexist.
 */

package vc

import (
	"errors"
	"fmt"
	"html"
	"log/slog"
	"os"
	"strings"
	"sync"

	"ashokshau/tgmusic/src/core"
	"ashokshau/tgmusic/src/core/cache"
	"ashokshau/tgmusic/src/utils"

	td "github.com/AshokShau/gotdbot"
)

type chatKey struct {
	BotID  int64
	ChatID int64
}

type ownerKey struct {
	Index  int
	ChatID int64
}

// assistantPool tracks, per chat, which assistant index each bot is using,
// so concurrent bots never collide on the same assistant in the same chat.
type assistantPool struct {
	mu sync.Mutex

	// (botID, chatID) -> assigned assistant index
	assignment map[chatKey]int
	// (index, chatID) -> owning botID (only one bot may hold a given
	// assistant in a given chat at a time)
	owner map[ownerKey]int64
	// (botID, chatID) -> true while that bot has an active stream there
	active map[chatKey]bool
}

var pool = &assistantPool{
	assignment: make(map[chatKey]int),
	owner:      make(map[ownerKey]int64),
	active:     make(map[chatKey]bool),
}

// ErrAllAssistantsBusy is returned by AssignFor when every assistant is
// already streaming for a different bot in the requested chat.
var ErrAllAssistantsBusy = errors.New(
	"⚠️ <b>All Assistants Are Busy In This Chat</b>\n\n" +
		"Every available assistant account is already streaming for another bot " +
		"in this group.\n\nPlease wait for an active stream to finish, or ask an " +
		"admin to add more assistant accounts.")

// AssignFor returns the assistant index this bot should use for this chat,
// reusing a prior assignment if one exists, or claiming a free one.
func (c *TelegramCalls) AssignFor(botID, chatID int64) (int, error) {
	c.mu.RLock()
	total := len(c.assistants)
	c.mu.RUnlock()

	if total == 0 {
		return -1, errors.New("no assistants are configured")
	}

	pool.mu.Lock()
	defer pool.mu.Unlock()

	key := chatKey{BotID: botID, ChatID: chatID}

	if idx, ok := pool.assignment[key]; ok && idx < total {
		pool.owner[ownerKey{Index: idx, ChatID: chatID}] = botID
		return idx, nil
	}

	// Prefer the assistant already picked for this chat by the "main"
	// single-key path (getClientIndex), if it isn't claimed by someone else.
	if idx, err := c.getClientIndex(chatID); err == nil {
		if o, taken := pool.owner[ownerKey{Index: idx, ChatID: chatID}]; !taken || o == botID {
			pool.assignment[key] = idx
			pool.owner[ownerKey{Index: idx, ChatID: chatID}] = botID
			return idx, nil
		}
	}

	// Least-loaded free assistant: not claimed by a DIFFERENT bot in this chat.
	best, bestLoad := -1, -1
	for idx := 0; idx < total; idx++ {
		if o, taken := pool.owner[ownerKey{Index: idx, ChatID: chatID}]; taken && o != botID {
			continue
		}
		load := pool.loadOf(idx)
		if best == -1 || load < bestLoad {
			best, bestLoad = idx, load
		}
	}

	if best == -1 {
		return -1, ErrAllAssistantsBusy
	}

	pool.assignment[key] = best
	pool.owner[ownerKey{Index: best, ChatID: chatID}] = botID
	return best, nil
}

// loadOf counts how many chats currently use assistant idx (across all
// bots). Caller must hold pool.mu.
func (p *assistantPool) loadOf(idx int) int {
	n := 0
	for k, active := range p.active {
		if active {
			if assignedIdx, ok := p.assignment[k]; ok && assignedIdx == idx {
				n++
			}
		}
	}
	return n
}

// ReleaseFor frees the (botID, chatID) claim, letting another bot use that
// assistant for this chat next time.
func (c *TelegramCalls) ReleaseFor(botID, chatID int64) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	key := chatKey{BotID: botID, ChatID: chatID}
	if idx, ok := pool.assignment[key]; ok {
		delete(pool.owner, ownerKey{Index: idx, ChatID: chatID})
	}
	delete(pool.assignment, key)
	delete(pool.active, key)
}

// IsActiveFor reports whether botID has a live stream in chatID.
func (c *TelegramCalls) IsActiveFor(botID, chatID int64) bool {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	return pool.active[chatKey{BotID: botID, ChatID: chatID}]
}

// ActiveChatsFor returns every chat ID botID currently has a live stream in.
// Used by /cstats to report per-clone (and main-bot) active VC counts.
func (c *TelegramCalls) ActiveChatsFor(botID int64) []int64 {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	var chats []int64
	for k, active := range pool.active {
		if active && k.BotID == botID {
			chats = append(chats, k.ChatID)
		}
	}
	return chats
}

// GetGroupAssistantFor is the bot-aware equivalent of GetGroupAssistant.
func (c *TelegramCalls) GetGroupAssistantFor(botID, chatID int64) (*Assistant, int, error) {
	idx, err := c.AssignFor(botID, chatID)
	if err != nil {
		return nil, -1, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	call, ok := c.assistants[idx]
	if !ok {
		return nil, -1, fmt.Errorf("no ntgcalls instance was found for client index %d", idx)
	}
	return call, idx, nil
}

// PlayMediaFor is the bot-aware equivalent of PlayMedia: it claims a
// dedicated assistant for (botID, chatID) — reporting ErrAllAssistantsBusy
// if none is free — instead of always reusing the chat's single default
// assistant. This is what lets the main bot and any number of clones stream
// independently in the same chat at once.
func (c *TelegramCalls) PlayMediaFor(botID int64, bot *td.Client, chatID int64, filePath string, video bool, ffmpegParameters string) error {
	tried := map[int]bool{}

	for attempt := 0; attempt < 3; attempt++ {
		idx, err := c.AssignFor(botID, chatID)
		if err != nil {
			return err
		}
		if tried[idx] {
			break
		}
		tried[idx] = true

		c.mu.RLock()
		call := c.assistants[idx]
		c.mu.RUnlock()
		if call == nil {
			continue
		}

		err = c.playMedia(bot, chatID, filePath, video, ffmpegParameters, call, idx)
		if err == nil {
			c.markActive(botID, chatID)
			return nil
		}

		if classifyError(err) == errFatal {
			return fatalMessage(err)
		}

		// Release this claim so the next attempt can pick a different
		// assistant instead of retrying the same broken one.
		pool.mu.Lock()
		delete(pool.owner, ownerKey{Index: idx, ChatID: chatID})
		delete(pool.assignment, chatKey{BotID: botID, ChatID: chatID})
		pool.mu.Unlock()
	}

	return errors.New("playback failed: no assistant could join this chat")
}

func (c *TelegramCalls) markActive(botID, chatID int64) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.active[chatKey{BotID: botID, ChatID: chatID}] = true
}

func (c *TelegramCalls) markInactive(botID, chatID int64) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	delete(pool.active, chatKey{BotID: botID, ChatID: chatID})
}

// StopFor stops botID's stream in chatID and releases its assistant claim.
func (c *TelegramCalls) StopFor(botID, chatID int64, banned bool) error {
	call, idx, err := c.GetGroupAssistantFor(botID, chatID)
	if err != nil {
		return err
	}
	err = call.stopCall(chatID, banned)
	c.markInactive(botID, chatID)
	c.ReleaseFor(botID, chatID)
	if err != nil {
		if containsAny(err.Error(), "not found") {
			return nil
		}
		return fmt.Errorf("failed to stop call: %w", err)
	}
	_ = idx
	return nil
}

// PauseFor pauses botID's stream in chatID.
func (c *TelegramCalls) PauseFor(botID, chatID int64) (bool, error) {
	call, _, err := c.GetGroupAssistantFor(botID, chatID)
	if err != nil {
		return false, err
	}
	return call.binding.Pause(chatID)
}

// ResumeFor resumes botID's stream in chatID.
func (c *TelegramCalls) ResumeFor(botID, chatID int64) (bool, error) {
	call, _, err := c.GetGroupAssistantFor(botID, chatID)
	if err != nil {
		return false, err
	}
	return call.binding.Resume(chatID)
}

// MuteFor mutes botID's stream in chatID.
func (c *TelegramCalls) MuteFor(botID, chatID int64) (bool, error) {
	call, _, err := c.GetGroupAssistantFor(botID, chatID)
	if err != nil {
		return false, err
	}
	return call.binding.Mute(chatID)
}

// UnmuteFor unmutes botID's stream in chatID.
func (c *TelegramCalls) UnmuteFor(botID, chatID int64) (bool, error) {
	call, _, err := c.GetGroupAssistantFor(botID, chatID)
	if err != nil {
		return false, err
	}
	return call.binding.UnMute(chatID)
}

// PlayedTimeFor returns the elapsed playback time of botID's stream in chatID.
func (c *TelegramCalls) PlayedTimeFor(botID, chatID int64) (uint64, error) {
	call, _, err := c.GetGroupAssistantFor(botID, chatID)
	if err != nil {
		return 0, err
	}
	return call.binding.Time(chatID, 0)
}

// SeekStreamFor is the bot-aware equivalent of SeekStream.
func (c *TelegramCalls) SeekStreamFor(botID int64, bot *td.Client, chatID int64, filePath string, toSeek, duration int, isVideo bool) error {
	if toSeek < 0 || duration <= 0 {
		return errors.New("invalid seek position or duration. The position must be positive and the duration must be greater than 0")
	}

	isURL := urlRegex.MatchString(filePath)
	_, statErr := os.Stat(filePath)
	isFile := statErr == nil

	var ffmpegParams string
	if isURL || !isFile {
		ffmpegParams = fmt.Sprintf("-ss %d -i %s -to %d", toSeek, filePath, duration)
	} else {
		ffmpegParams = fmt.Sprintf("-ss %d -to %d", toSeek, duration)
	}

	return c.PlayMediaFor(botID, bot, chatID, filePath, isVideo, ffmpegParams)
}

// PlayNextFor is the bot-aware equivalent of PlayNext, used so that when a
// clone's (or the main bot's) song ends, the next queued track keeps
// playing through that SAME bot's dedicated assistant rather than
// re-triggering the single-key assignment path.
func (c *TelegramCalls) PlayNextFor(botID int64, bot *td.Client, chatID int64) error {
	loop := cache.ChatCache.GetLoopCount(chatID)
	if loop > 0 {
		cache.ChatCache.SetLoopCount(chatID, loop-1)
		if current := cache.ChatCache.GetPlayingTrack(chatID); current != nil {
			return c.playSongFor(botID, bot, chatID, current)
		}
	}

	if next := cache.ChatCache.GetUpcomingTrack(chatID); next != nil {
		cache.ChatCache.RemoveCurrentSong(chatID)
		return c.playSongFor(botID, bot, chatID, next)
	}

	cache.ChatCache.RemoveCurrentSong(chatID)
	c.markInactive(botID, chatID)
	c.ReleaseFor(botID, chatID)
	_, _ = bot.SendTextMessage(chatID, "🎵 Queue finished. Add more songs with /play.", nil)
	return nil
}

// playSongFor is the bot-aware equivalent of playSong.
func (c *TelegramCalls) playSongFor(botID int64, bot *td.Client, chatID int64, song *utils.CachedTrack) error {
	reply, err := bot.SendTextMessage(chatID, fmt.Sprintf("Downloading %s...", song.Name), nil)
	if err != nil {
		return err
	}

	if err = c.downloadAndPrepareSong(bot, song, reply); err != nil {
		return c.PlayNextFor(botID, bot, chatID)
	}

	if err = c.PlayMediaFor(botID, bot, chatID, song.FilePath, song.IsVideo, ""); err != nil {
		_, _ = reply.EditText(bot, err.Error(), &td.EditTextMessageOpts{ParseMode: "HTML", DisableWebPagePreview: true})
		return nil
	}

	if song.Duration == 0 {
		song.Duration = utils.GetMediaDuration(song.FilePath)
	}

	text := fmt.Sprintf(
		"<u><b>| Started streaming</b></u>\n\n<b>Title:</b> <a href='%s'>%s</a>\n\n<b>Duration:</b> %s min\n<b>Requested by:</b> %s",
		html.EscapeString(song.URL),
		html.EscapeString(song.Name),
		utils.SecToMin(song.Duration),
		html.EscapeString(song.User),
	)

	_, err = reply.EditText(bot, text, &td.EditTextMessageOpts{
		ReplyMarkup:           core.ControlButtons("play"),
		ParseMode:             "HTML",
		DisableWebPagePreview: true,
	})
	if err != nil {
		slog.Info("[playSongFor] Failed to edit message", "error", err)
	}
	return nil
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
