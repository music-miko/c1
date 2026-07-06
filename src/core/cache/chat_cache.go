/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package cache

import (
	"ashokshau/tgmusic/src/utils"
	"sync"
)

// ChatData holds the state of a chat's music queue.
type ChatData struct {
	Queue []*utils.CachedTrack
}

// chatKey scopes a queue to a specific bot's view of a chat, so the main
// bot and any number of clones can each have their own independent queue
// and "now playing" state in the SAME chat without clobbering each other.
type chatKey struct {
	BotID  int64
	ChatID int64
}

// ChatCacher is a thread-safe cache that manages music queues for multiple
// (bot, chat) pairs.
type ChatCacher struct {
	mu        sync.RWMutex
	chatCache map[chatKey]*ChatData
}

// newChatCacher initializes and returns a new ChatCacher.
func newChatCacher() *ChatCacher {
	return &ChatCacher{
		chatCache: make(map[chatKey]*ChatData),
	}
}

// getOrCreate returns the ChatData for a (bot, chat) pair, creating it if
// absent. Caller must hold the write lock.
func (c *ChatCacher) getOrCreate(key chatKey) *ChatData {
	data, ok := c.chatCache[key]
	if !ok {
		data = &ChatData{}
		c.chatCache[key] = data
	}
	return data
}

// ============================================================================
// Legacy chat-only API (kept for backward compatibility / tests). These all
// operate on a reserved BotID=0 bucket, distinct from every real bot ID, so
// they can't collide with any bot-scoped state. New code should use the
// *For variants below instead.
// ============================================================================

func (c *ChatCacher) AddSong(chatID int64, song *utils.CachedTrack) int {
	return c.AddSongFor(0, chatID, song)
}

func (c *ChatCacher) AddSongs(chatID int64, songs []*utils.CachedTrack) int {
	return c.AddSongsFor(0, chatID, songs)
}

func (c *ChatCacher) GetPlayingTrack(chatID int64) *utils.CachedTrack {
	return c.GetPlayingTrackFor(0, chatID)
}

func (c *ChatCacher) GetUpcomingTrack(chatID int64) *utils.CachedTrack {
	return c.GetUpcomingTrackFor(0, chatID)
}

func (c *ChatCacher) RemoveCurrentSong(chatID int64) *utils.CachedTrack {
	return c.RemoveCurrentSongFor(0, chatID)
}

func (c *ChatCacher) RemoveTrack(chatID int64, index int) bool {
	return c.RemoveTrackFor(0, chatID, index)
}

func (c *ChatCacher) IsActive(chatID int64) bool {
	return c.IsActiveFor(0, chatID)
}

func (c *ChatCacher) ClearChat(chatID int64) {
	c.ClearChatFor(0, chatID)
}

func (c *ChatCacher) GetQueueLength(chatID int64) int {
	return c.GetQueueLengthFor(0, chatID)
}

func (c *ChatCacher) GetLoopCount(chatID int64) int {
	return c.GetLoopCountFor(0, chatID)
}

func (c *ChatCacher) SetLoopCount(chatID int64, loop int) bool {
	return c.SetLoopCountFor(0, chatID, loop)
}

func (c *ChatCacher) GetQueue(chatID int64) []*utils.CachedTrack {
	return c.GetQueueFor(0, chatID)
}

func (c *ChatCacher) GetActiveChats() []int64 {
	return c.GetActiveChatsFor(0)
}

func (c *ChatCacher) GetTrackIfExists(chatID int64, trackID string) *utils.CachedTrack {
	return c.GetTrackIfExistsFor(0, chatID, trackID)
}

// ============================================================================
// Bot-scoped API — every handler/vc call site should use these.
// ============================================================================

// AddSongFor adds a track to (botID, chatID)'s queue and returns the new queue length.
func (c *ChatCacher) AddSongFor(botID, chatID int64, song *utils.CachedTrack) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	data := c.getOrCreate(chatKey{botID, chatID})
	data.Queue = append(data.Queue, song)
	return len(data.Queue)
}

// AddSongsFor appends multiple tracks to (botID, chatID)'s queue and returns the new queue length.
func (c *ChatCacher) AddSongsFor(botID, chatID int64, songs []*utils.CachedTrack) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	data := c.getOrCreate(chatKey{botID, chatID})
	data.Queue = append(data.Queue, songs...)
	return len(data.Queue)
}

// GetPlayingTrackFor returns the first track in (botID, chatID)'s queue, or nil if empty.
func (c *ChatCacher) GetPlayingTrackFor(botID, chatID int64) *utils.CachedTrack {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, ok := c.chatCache[chatKey{botID, chatID}]
	if !ok || len(data.Queue) == 0 {
		return nil
	}
	return data.Queue[0]
}

// GetUpcomingTrackFor returns the second track in (botID, chatID)'s queue, or nil if fewer than two exist.
func (c *ChatCacher) GetUpcomingTrackFor(botID, chatID int64) *utils.CachedTrack {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, ok := c.chatCache[chatKey{botID, chatID}]
	if !ok || len(data.Queue) < 2 {
		return nil
	}
	return data.Queue[1]
}

// RemoveCurrentSongFor removes and returns the currently playing track for (botID, chatID), or nil if empty.
func (c *ChatCacher) RemoveCurrentSongFor(botID, chatID int64) *utils.CachedTrack {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, ok := c.chatCache[chatKey{botID, chatID}]
	if !ok || len(data.Queue) == 0 {
		return nil
	}

	removed := data.Queue[0]
	data.Queue[0] = nil
	data.Queue = data.Queue[1:]
	return removed
}

// RemoveTrackFor removes the track at the given index for (botID, chatID) and returns whether it succeeded.
func (c *ChatCacher) RemoveTrackFor(botID, chatID int64, index int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, ok := c.chatCache[chatKey{botID, chatID}]
	if !ok || index < 0 || index >= len(data.Queue) {
		return false
	}

	q := data.Queue
	copy(q[index:], q[index+1:])
	q[len(q)-1] = nil
	data.Queue = q[:len(q)-1]
	return true
}

// IsActiveFor returns true if (botID, chatID) has at least one queued track.
func (c *ChatCacher) IsActiveFor(botID, chatID int64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, ok := c.chatCache[chatKey{botID, chatID}]
	return ok && len(data.Queue) > 0
}

// ClearChatFor deletes all queued tracks for (botID, chatID).
func (c *ChatCacher) ClearChatFor(botID, chatID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := chatKey{botID, chatID}
	if data, ok := c.chatCache[key]; ok {
		for i := range data.Queue {
			data.Queue[i] = nil
		}
		delete(c.chatCache, key)
	}
}

// GetQueueLengthFor returns the number of tracks queued for (botID, chatID).
func (c *ChatCacher) GetQueueLengthFor(botID, chatID int64) int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, ok := c.chatCache[chatKey{botID, chatID}]
	if !ok {
		return 0
	}
	return len(data.Queue)
}

// GetLoopCountFor returns the loop count of the currently playing track for (botID, chatID), or 0 if none.
func (c *ChatCacher) GetLoopCountFor(botID, chatID int64) int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, ok := c.chatCache[chatKey{botID, chatID}]
	if !ok || len(data.Queue) == 0 {
		return 0
	}
	return data.Queue[0].Loop
}

// SetLoopCountFor sets the loop count on the currently playing track for (botID, chatID).
// Returns false if there is no active track.
func (c *ChatCacher) SetLoopCountFor(botID, chatID int64, loop int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, ok := c.chatCache[chatKey{botID, chatID}]
	if !ok || len(data.Queue) == 0 {
		return false
	}
	data.Queue[0].Loop = loop
	return true
}

// GetQueueFor returns a shallow copy of the queue for (botID, chatID).
func (c *ChatCacher) GetQueueFor(botID, chatID int64) []*utils.CachedTrack {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, ok := c.chatCache[chatKey{botID, chatID}]
	if !ok || len(data.Queue) == 0 {
		return nil
	}
	return append([]*utils.CachedTrack(nil), data.Queue...)
}

// IsActiveAny reports whether ANY bot (main or any clone) has an active
// queue in chatID. Used by cross-bot maintenance routines (like the
// assistant's auto-leave sweep) that aren't scoped to a single bot.
func (c *ChatCacher) IsActiveAny(chatID int64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for key, data := range c.chatCache {
		if key.ChatID == chatID && len(data.Queue) > 0 {
			return true
		}
	}
	return false
}

// GetActiveChatsFor returns the chat IDs where botID has at least one queued track.
func (c *ChatCacher) GetActiveChatsFor(botID int64) []int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var active []int64
	for key, data := range c.chatCache {
		if key.BotID == botID && len(data.Queue) > 0 {
			active = append(active, key.ChatID)
		}
	}
	return active
}

// GetTrackIfExistsFor searches (botID, chatID)'s queue for a track by ID and returns it, or nil if not found.
func (c *ChatCacher) GetTrackIfExistsFor(botID, chatID int64, trackID string) *utils.CachedTrack {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, ok := c.chatCache[chatKey{botID, chatID}]
	if !ok {
		return nil
	}
	for _, t := range data.Queue {
		if t.TrackID == trackID {
			return t
		}
	}
	return nil
}

// ChatCache is the global instance.
var ChatCache = newChatCacher()
