/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 *
 * Clone-bot command handlers.
 *
 *  - /clone /myclone /deleteclone /stopclone /cstats are registered ONLY on
 *    the PRIMARY bot client (see LoadCloneModules in clone_load.go) — a
 *    clone can't spawn further clones, mirroring anony/plugins/clone.py.
 *
 *  - /edit is registered ONLY on each CLONE client itself (see
 *    LoadCloneOwnerModules in clone_load.go) — the clone owner DMs their own
 *    bot to configure it, mirroring anony/clone/plugins/edit.py.
 */

package handlers

import (
	"fmt"
	"strconv"
	"strings"

	"ashokshau/tgmusic/src/clone"
	"ashokshau/tgmusic/src/core/db"
	"ashokshau/tgmusic/src/vc"

	td "github.com/AshokShau/gotdbot"
)

func cloneBtn(text, data string) td.InlineKeyboardButton {
	return td.InlineKeyboardButton{Text: text, Type: &td.InlineKeyboardButtonTypeCallback{Data: []byte(data)}}
}

func urlBtn(text, link string) td.InlineKeyboardButton {
	return td.InlineKeyboardButton{Text: text, Type: &td.InlineKeyboardButtonTypeUrl{Url: link}}
}

// ============================================================================
// /clone /myclone /deleteclone /stopclone /cstats — primary bot only
// ============================================================================

// cloneCmdHandler handles /clone [token] in private chat.
func cloneCmdHandler(c *td.Client, m *td.Message) error {
	if !m.IsPrivate() {
		_, _ = m.ReplyText(c, "Clone commands only work in private chat. Send me a DM to create your clone bot.", &td.SendTextMessageOpts{
			ReplyMarkup: &td.ReplyMarkupInlineKeyboard{Rows: [][]td.InlineKeyboardButton{
				{urlBtn("Open DM", fmt.Sprintf("https://t.me/%s?start=clone", c.Me.Usernames.EditableUsername))},
			}},
		})
		return td.EndGroups
	}

	userID := m.SenderID()
	isOwner := isDev(c, m)

	if !isOwner {
		existing, _ := db.Instance.GetCloneByOwner(userID)
		if existing != nil && existing.Active {
			_, running := clone.Instance.Get(existing.ID)
			status := "Stopped"
			if running {
				status = "Running"
			}
			_, _ = m.ReplyText(c, fmt.Sprintf(
				"<b>You already have a clone bot.</b>\n\nBot: @%s   Status: %s\n\nOnly one clone bot per account. Use /deleteclone to remove it first.",
				existing.Username, status,
			), &td.SendTextMessageOpts{
				ParseMode: "HTML",
				ReplyMarkup: &td.ReplyMarkupInlineKeyboard{Rows: [][]td.InlineKeyboardButton{
					{urlBtn("Open @"+existing.Username, "https://t.me/"+existing.Username)},
					{cloneBtn("Delete Clone", fmt.Sprintf("clone_del %d", existing.ID))},
				}},
			})
			return td.EndGroups
		}
	}

	token := clone.ExtractToken(Args(m))
	if token == "" {
		note := "\n\n<i>One clone per account.</i>"
		if isOwner {
			note = "\n\n<i>✨ You're a bot admin — unlimited clones allowed.</i>"
		}
		_, _ = m.ReplyText(c,
			"<b>Clone Your Music Bot</b>\n\n"+
				"<b>Step 1 —</b> Talk to @BotFather\n"+
				"<b>Step 2 —</b> Send /newbot and follow the steps\n"+
				"<b>Step 3 —</b> Copy the API token from BotFather\n"+
				"<b>Step 4 —</b> Forward the BotFather message here, or send:\n<code>/clone YOUR_TOKEN</code>"+note,
			&td.SendTextMessageOpts{
				ParseMode: "HTML",
				ReplyMarkup: &td.ReplyMarkupInlineKeyboard{Rows: [][]td.InlineKeyboardButton{
					{cloneBtn("Close", "clone_close")},
				}},
			})
		return td.EndGroups
	}

	processCloneToken(c, m, userID, token)
	return td.EndGroups
}

// forwardedTokenHandler watches for a forwarded BotFather message in DM and
// extracts the token from it automatically.
func forwardedTokenHandler(c *td.Client, m *td.Message) error {
	text := m.GetText()
	if text == "" || (!strings.Contains(text, "Use this token") && !strings.Contains(text, "HTTP API")) {
		return nil
	}
	token := clone.ExtractToken(text)
	if token == "" {
		return nil
	}

	userID := m.SenderID()
	isOwner := isDev(c, m)
	if !isOwner {
		existing, _ := db.Instance.GetCloneByOwner(userID)
		if existing != nil && existing.Active {
			_, _ = m.ReplyText(c, fmt.Sprintf("You already have @%s.\nUse /deleteclone to remove it first.", existing.Username), nil)
			return td.EndGroups
		}
	}

	processCloneToken(c, m, userID, token)
	return td.EndGroups
}

func processCloneToken(c *td.Client, m *td.Message, userID int64, token string) {
	sent, err := m.ReplyText(c, "Validating token…", nil)
	if err != nil {
		return
	}

	info, err := clone.ValidateToken(token)
	if err != nil {
		_, _ = sent.EditText(c, "<b>Invalid bot token.</b>\n\nCopy the full token from @BotFather and try again.", &td.EditTextMessageOpts{ParseMode: "HTML"})
		return
	}

	existing, _ := db.Instance.GetClone(info.ID)
	if existing != nil && existing.Active {
		_, _ = sent.EditText(c, "This token is already registered by another user.", nil)
		return
	}

	_, _ = sent.EditText(c, fmt.Sprintf("Token valid. Starting @%s…", info.Username), nil)

	if err := db.Instance.RegisterClone(info.ID, userID, token, info.Username, info.FirstName); err != nil {
		_, _ = sent.EditText(c, "Failed to save clone registration. Try again later.", nil)
		return
	}

	if _, err := clone.Instance.Start(info.ID, token); err != nil {
		_ = db.Instance.DeactivateClone(info.ID)
		_, _ = sent.EditText(c, fmt.Sprintf(
			"<b>Failed to start clone bot.</b>\nReason: <code>%s</code>\n\nMake sure the token is correct and not used elsewhere.",
			err.Error(),
		), &td.EditTextMessageOpts{ParseMode: "HTML"})
		return
	}

	_, _ = sent.EditText(c, fmt.Sprintf(
		"<b>Clone Bot Started!</b>\n\nBot: @%s\n\nDM your bot directly and send /edit to configure logger, start image and custom buttons.\nUse /deleteclone here to delete your clone.",
		info.Username,
	), &td.EditTextMessageOpts{
		ParseMode: "HTML",
		ReplyMarkup: &td.ReplyMarkupInlineKeyboard{Rows: [][]td.InlineKeyboardButton{
			{urlBtn("Open @"+info.Username, "https://t.me/"+info.Username)},
		}},
	})
}

// myCloneCmdHandler handles /mybot and /myclone.
func myCloneCmdHandler(c *td.Client, m *td.Message) error {
	userID := m.SenderID()
	existing, err := db.Instance.GetCloneByOwner(userID)
	if err != nil || existing == nil || !existing.Active {
		_, _ = m.ReplyText(c, "You don't have any clone bots.\nUse /clone to create one.", nil)
		return td.EndGroups
	}

	_, running := clone.Instance.Get(existing.ID)
	status := "Stopped"
	activeVC := 0
	if running {
		status = "Running"
		activeVC = len(vc.Calls.ActiveChatsFor(existing.ID))
	}

	_, _ = m.ReplyText(c, fmt.Sprintf(
		"<b>Your Clone Bot</b>\n\nBot: @%s\nStatus: %s\nActive VC: <code>%d</code>",
		existing.Username, status, activeVC,
	), &td.SendTextMessageOpts{
		ParseMode: "HTML",
		ReplyMarkup: &td.ReplyMarkupInlineKeyboard{Rows: [][]td.InlineKeyboardButton{
			{urlBtn("Open @"+existing.Username, "https://t.me/"+existing.Username)},
			{cloneBtn("Delete Clone", fmt.Sprintf("clone_del %d", existing.ID))},
		}},
	})
	return td.EndGroups
}

// deleteCloneCmdHandler handles /deleteclone and /removeclone.
func deleteCloneCmdHandler(c *td.Client, m *td.Message) error {
	userID := m.SenderID()

	if isDev(c, m) {
		if arg := strings.TrimPrefix(strings.TrimSpace(Args(m)), "@"); arg != "" {
			all, _ := db.Instance.GetAllClones()
			for _, cl := range all {
				if strconv.FormatInt(cl.ID, 10) == arg || strings.EqualFold(cl.Username, arg) {
					askDeleteConfirm(c, m, cl.ID, cl.Username, true)
					return td.EndGroups
				}
			}
			_, _ = m.ReplyText(c, "Clone not found. Provide bot_id or @username.", nil)
			return td.EndGroups
		}
	}

	existing, err := db.Instance.GetCloneByOwner(userID)
	if err != nil || existing == nil {
		_, _ = m.ReplyText(c, "You don't have any clone bot.", nil)
		return td.EndGroups
	}
	askDeleteConfirm(c, m, existing.ID, existing.Username, false)
	return td.EndGroups
}

func askDeleteConfirm(c *td.Client, m *td.Message, botID int64, username string, sudo bool) {
	suffix := ""
	if sudo {
		suffix = " (admin action)"
	}
	_, _ = m.ReplyText(c, fmt.Sprintf(
		"<b>Delete Clone Bot%s</b>\n\nBot: @%s\n\nThis will <b>permanently delete</b> the clone registration and stop it. Are you sure?",
		suffix, username,
	), &td.SendTextMessageOpts{
		ParseMode: "HTML",
		ReplyMarkup: &td.ReplyMarkupInlineKeyboard{Rows: [][]td.InlineKeyboardButton{
			{cloneBtn("Yes, Delete", fmt.Sprintf("clone_delconfirm %d", botID)), cloneBtn("Cancel", "clone_delcancel")},
		}},
	})
}

// stopCloneCmdHandler handles /stopclone <bot_id|@username> (admin-only).
func stopCloneCmdHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}
	arg := strings.TrimPrefix(strings.TrimSpace(Args(m)), "@")
	if arg == "" {
		_, _ = m.ReplyText(c, "Usage: /stopclone <bot_id or @username>", nil)
		return td.EndGroups
	}
	for botID, cl := range clone.Instance.List() {
		if strconv.FormatInt(botID, 10) == arg || cl.Me.Usernames.EditableUsername == arg {
			clone.Instance.Stop(botID)
			_ = db.Instance.DeactivateClone(botID)
			_, _ = m.ReplyText(c, fmt.Sprintf("@%s stopped.", cl.Me.Usernames.EditableUsername), nil)
			return td.EndGroups
		}
	}
	_, _ = m.ReplyText(c, "No matching active clone found.", nil)
	return td.EndGroups
}

// cstatsCmdHandler handles /cstats (admin-only) — replaces the old
// /activeclones and /clonestats commands with a single combined view:
// total/active clone counts, each running clone's active VC count, and the
// main bot's own active VC count.
func cstatsCmdHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}

	total, activeInDB, err := db.Instance.CountClones()
	if err != nil {
		_, _ = m.ReplyText(c, "Failed to fetch clone stats.", nil)
		return td.EndGroups
	}

	running := clone.Instance.List()
	mainVC := len(vc.Calls.ActiveChatsFor(c.Me.Id))

	var sb strings.Builder
	sb.WriteString("<b>Clone Statistics</b>\n\n")
	sb.WriteString(fmt.Sprintf("<b>Main bot active VC:</b> <code>%d</code>\n\n", mainVC))
	sb.WriteString(fmt.Sprintf("<b>Total clones:</b> <code>%d</code>\n", total))
	sb.WriteString(fmt.Sprintf("<b>Active clones (DB):</b> <code>%d</code>\n", activeInDB))
	sb.WriteString(fmt.Sprintf("<b>Running clones:</b> <code>%d</code>\n\n", len(running)))

	if len(running) == 0 {
		sb.WriteString("<i>No clones currently running.</i>")
	} else {
		sb.WriteString("<b>Per-clone active VC:</b>\n")
		for botID, cl := range running {
			vcCount := len(vc.Calls.ActiveChatsFor(botID))
			sb.WriteString(fmt.Sprintf("• @%s — <code>%d</code> active VC\n", cl.Me.Usernames.EditableUsername, vcCount))
		}
	}

	_, _ = m.ReplyText(c, sb.String(), &td.SendTextMessageOpts{ParseMode: "HTML"})
	return td.EndGroups
}

// cloneCallbackHandler handles clone_* callback buttons (main bot only).
func cloneCallbackHandler(c *td.Client, cb *td.UpdateNewCallbackQuery) error {
	data := cb.DataString()
	fields := strings.Fields(data)
	if len(fields) == 0 {
		return nil
	}

	switch fields[0] {
	case "clone_close":
		_, _ = c.DeleteMessages(cb.ChatId, []int64{cb.MessageId}, &td.DeleteMessagesOpts{Revoke: true})

	case "clone_del":
		if len(fields) < 2 {
			return nil
		}
		botID, _ := strconv.ParseInt(fields[1], 10, 64)
		reg, _ := db.Instance.GetClone(botID)
		if reg == nil {
			_ = cb.Answer(c, 0, true, "Clone not found.", "")
			return nil
		}
		if reg.OwnerID != cb.SenderUserId && !isDevID(cb.SenderUserId) {
			_ = cb.Answer(c, 0, true, "Not your clone bot.", "")
			return nil
		}
		_, _ = cb.EditMessageText(c, fmt.Sprintf(
			"<b>Confirm Deletion</b>\n\nDelete @%s and its data permanently?", reg.Username,
		), &td.EditTextMessageOpts{
			ParseMode: "HTML",
			ReplyMarkup: &td.ReplyMarkupInlineKeyboard{Rows: [][]td.InlineKeyboardButton{
				{cloneBtn("Yes, Delete", fmt.Sprintf("clone_delconfirm %d", botID)), cloneBtn("Cancel", "clone_delcancel")},
			}},
		})

	case "clone_delconfirm":
		if len(fields) < 2 {
			return nil
		}
		botID, _ := strconv.ParseInt(fields[1], 10, 64)
		reg, _ := db.Instance.GetClone(botID)
		if reg == nil {
			_ = cb.Answer(c, 0, true, "Clone not found.", "")
			return nil
		}
		if reg.OwnerID != cb.SenderUserId && !isDevID(cb.SenderUserId) {
			_ = cb.Answer(c, 0, true, "Not your clone bot.", "")
			return nil
		}
		_, _ = cb.EditMessageText(c, "Deleting…", nil)
		clone.Instance.Stop(botID)
		_ = db.Instance.DeleteCloneAllData(botID)
		_, _ = cb.EditMessageText(c, fmt.Sprintf("@%s deleted. All data removed.", reg.Username), nil)

	case "clone_delcancel":
		_, _ = cb.EditMessageText(c, "Deletion cancelled.", nil)
	}

	return nil
}

func isDevID(userID int64) bool {
	fake := &td.Message{SenderId: &td.MessageSenderUser{UserId: userID}}
	return isDev(nil, fake)
}
