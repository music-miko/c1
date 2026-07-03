/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 *
 * Package clone manages user-owned "clone" bot instances: bots spun up at
 * runtime from a /clone-supplied bot token, sharing the same assistant pool,
 * database and command set as the primary bot.
 */

package clone

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"ashokshau/tgmusic/src/core/db"

	td "github.com/AshokShau/gotdbot"
)

// Manager owns every currently-running clone bot client.
type Manager struct {
	tdMgr   *td.ClientManager
	apiID   int32
	apiHash string
	dbDir   string

	// setup is called once on every clone client right after it starts,
	// analogous to Python's `mod.setup(bot)` plugin loop. In practice this
	// is wired to handlers.LoadModules so clones get the full command set.
	setup func(*td.Client)

	mu     sync.RWMutex
	active map[int64]*td.Client // botID -> running client
}

// Instance is the process-wide clone manager singleton.
var Instance *Manager

// Init wires the clone manager to the shared gotdbot.ClientManager. Call
// this once from main.go, after the primary client has been registered.
func Init(tdMgr *td.ClientManager, apiID int32, apiHash, dbDir string, setup func(*td.Client)) {
	Instance = &Manager{
		tdMgr:   tdMgr,
		apiID:   apiID,
		apiHash: apiHash,
		dbDir:   dbDir,
		setup:   setup,
		active:  make(map[int64]*td.Client),
	}
}

// Start registers, boots and wires up a clone bot for the given token.
// botID must be the numeric bot user ID (the part before ':' in the token).
func (m *Manager) Start(botID int64, token string) (*td.Client, error) {
	m.mu.RLock()
	if c, ok := m.active[botID]; ok {
		m.mu.RUnlock()
		return c, nil
	}
	m.mu.RUnlock()

	cfg := td.DefaultClientConfig()
	cfg.DatabaseDirectory = fmt.Sprintf("%s_clone_%d", strings.TrimRight(m.dbDir, "/"), botID)
	_ = os.RemoveAll(cfg.DatabaseDirectory)

	client, err := m.tdMgr.RegisterClient(m.apiID, m.apiHash, token, cfg)
	if err != nil {
		return nil, err
	}

	if m.setup != nil {
		m.setup(client)
	}

	m.mu.Lock()
	m.active[botID] = client
	m.mu.Unlock()

	slog.Info("[clone] started", "bot_id", botID, "username", client.Me.Usernames.EditableUsername)
	return client, nil
}

// Stop gracefully stops and unregisters a running clone.
func (m *Manager) Stop(botID int64) {
	m.mu.Lock()
	client, ok := m.active[botID]
	if ok {
		delete(m.active, botID)
	}
	m.mu.Unlock()

	if !ok {
		return
	}

	client.Close()
	m.tdMgr.RemoveClient(client)
	slog.Info("[clone] stopped", "bot_id", botID)
}

// Get returns the running client for a clone, if any.
func (m *Manager) Get(botID int64) (*td.Client, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.active[botID]
	return c, ok
}

// List returns every currently-running clone client.
func (m *Manager) List() map[int64]*td.Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[int64]*td.Client, len(m.active))
	for k, v := range m.active {
		out[k] = v
	}
	return out
}

// Count returns the number of clones currently running.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.active)
}

// RestoreAll re-starts every clone marked active in the database. Call this
// once at boot, after Init.
func (m *Manager) RestoreAll() {
	clones, err := db.Instance.GetAllActiveClones()
	if err != nil {
		slog.Error("[clone] failed to load clones from db", "error", err)
		return
	}

	for _, cl := range clones {
		go func(botID int64, token, username string) {
			if _, err := m.Start(botID, token); err != nil {
				slog.Error("[clone] failed to restore clone", "bot_id", botID, "username", username, "error", err)
				_ = db.Instance.DeactivateClone(botID)
				return
			}
		}(cl.ID, cl.Token, cl.Username)
	}

	slog.Info("[clone] restoring clones", "count", len(clones))
}
