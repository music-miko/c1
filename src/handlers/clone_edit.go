/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 *
 * /edit lets a clone owner (or a user they've added with /addsudo-style
 * clone sudo — see db.AddCloneSudo) configure their clone bot: the log
 * channel, the /start image, and up to 5 custom /start buttons. This
 * mirrors anony/clone/plugins/edit.py. Registered only on clone clients
 * (see LoadCloneOwnerModules in clone_load.go) — a clone owner edits their
 * bot by DM'ing that bot directly, not the main bot.
 */

package handlers

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"ashokshau/tgmusic/src/core/db"

	td "github.com/AshokShau/gotdbot"
)

type editStep string

const (
	editStepNone      editStep = ""
	editStepLoggerID  editStep = "logger_id"
	editStepStartImg  editStep = "start_img"
	editStepAddButton editStep = "add_button"
)

var (
	editStateMu sync.Mutex
	editState   = make(map[int64]editStep) // userID -> pending step
)

func setEditState(userID int64, step editStep) {
	editStateMu.Lock()
	defer editStateMu.Unlock()
	if step == editStepNone {
		delete(editState, userID)
	} else {
		editState[userID] = step
	}
}

func getEditState(userID int64) editStep {
	editStateMu.Lock()
	defer editStateMu.Unlock()
	return editState[userID]
}

// canEdit reports whether the sender may configure this clone.
func canEdit(c *td.Client, m *td.Message) bool {
	ok, _ := db.Instance.IsCloneSudo(c.Me.Id, m.SenderID())
	return ok
}

// editCmdHandler handles /edit on a clone bot.
func editCmdHandler(c *td.Client, m *td.Message) error {
	if !m.IsPrivate() {
		return td.EndGroups
	}
	if !canEdit(c, m) {
		_, _ = m.ReplyText(c, "Only the owner of this clone bot can use /edit.", nil)
		return td.EndGroups
	}
	setEditState(m.SenderID(), editStepNone)
	_, _ = m.ReplyText(c, editMainMenuText(), &td.SendTextMessageOpts{ParseMode: "HTML", ReplyMarkup: editMainMenu()})
	return td.EndGroups
}

func editMainMenuText() string {
	return "<b>⚙️ Bot Settings</b>\n\nConfigure your clone bot below."
}

func editMainMenu() *td.ReplyMarkupInlineKeyboard {
	return &td.ReplyMarkupInlineKeyboard{Rows: [][]td.InlineKeyboardButton{
		{cloneBtn("📋 Logger", "cedit_logger"), cloneBtn("🖼 Start Image", "cedit_img")},
		{cloneBtn("🔘 Custom Buttons", "cedit_btn")},
		{cloneBtn("✖ Close", "cedit_close")},
	}}
}

// editCallbackHandler handles cedit_* callback buttons on a clone bot.
func editCallbackHandler(c *td.Client, cb *td.UpdateNewCallbackQuery) error {
	if !cb.IsPrivate() {
		return nil
	}

	// Re-check permission using a lightweight fake message so canEdit's
	// signature can stay message-based.
	fake := &td.Message{SenderId: &td.MessageSenderUser{UserId: cb.SenderUserId}}
	if !canEdit(c, fake) {
		_ = cb.Answer(c, 0, true, "Only the owner of this clone bot can use this.", "")
		return nil
	}

	data := cb.DataString()
	userID := cb.SenderUserId

	switch {
	case data == "cedit_close":
		setEditState(userID, editStepNone)
		_ = c.DeleteMessages(cb.ChatId, []int64{cb.MessageId}, &td.DeleteMessagesOpts{Revoke: true})

	case data == "cedit_back":
		setEditState(userID, editStepNone)
		_, _ = cb.EditMessageText(c, editMainMenuText(), &td.EditTextMessageOpts{ParseMode: "HTML", ReplyMarkup: editMainMenu()})

	case data == "cedit_logger":
		showLoggerMenu(c, cb)

	case data == "cedit_logger_set":
		setEditState(userID, editStepLoggerID)
		_, _ = cb.EditMessageText(c,
			"Send the numeric chat ID of the log channel (e.g. <code>-1001234567890</code>), or forward any message from that channel.\n\nMake sure this bot is an admin there.",
			&td.EditTextMessageOpts{ParseMode: "HTML", ReplyMarkup: &td.ReplyMarkupInlineKeyboard{Rows: [][]td.InlineKeyboardButton{{cloneBtn("« Back", "cedit_logger")}}}})

	case data == "cedit_logger_toggle":
		enabled, _ := db.Instance.GetCloneLoggerEnabled(c.Me.Id)
		_ = db.Instance.SetCloneLoggerEnabled(c.Me.Id, !enabled)
		showLoggerMenu(c, cb)

	case data == "cedit_logger_remove":
		_ = db.Instance.SetCloneLogger(c.Me.Id, 0)
		showLoggerMenu(c, cb)

	case data == "cedit_img":
		showImageMenu(c, cb)

	case data == "cedit_img_set":
		setEditState(userID, editStepStartImg)
		_, _ = cb.EditMessageText(c,
			"Send an image URL, or send a photo directly, to use as your bot's /start image.",
			&td.EditTextMessageOpts{ReplyMarkup: &td.ReplyMarkupInlineKeyboard{Rows: [][]td.InlineKeyboardButton{{cloneBtn("« Back", "cedit_img")}}}})

	case data == "cedit_img_remove":
		_ = db.Instance.SetCloneStartImg(c.Me.Id, "")
		showImageMenu(c, cb)

	case data == "cedit_btn":
		showButtonMenu(c, cb)

	case data == "cedit_btn_add":
		cl, _ := db.Instance.GetClone(c.Me.Id)
		if cl != nil && len(cl.CustomButtons) >= 5 {
			_ = cb.Answer(c, 0, true, "Maximum of 5 custom buttons reached.", "")
			return nil
		}
		setEditState(userID, editStepAddButton)
		_, _ = cb.EditMessageText(c,
			"Send the button in this format:\n<code>Button Text | https://example.com</code>",
			&td.EditTextMessageOpts{ParseMode: "HTML", ReplyMarkup: &td.ReplyMarkupInlineKeyboard{Rows: [][]td.InlineKeyboardButton{{cloneBtn("« Back", "cedit_btn")}}}})

	case strings.HasPrefix(data, "cedit_btn_rm_"):
		idxStr := strings.TrimPrefix(data, "cedit_btn_rm_")
		idx, err := strconv.Atoi(idxStr)
		if err == nil {
			_ = db.Instance.RemoveCustomButton(c.Me.Id, idx)
		}
		showButtonMenu(c, cb)
	}

	return nil
}

func showLoggerMenu(c *td.Client, cb *td.UpdateNewCallbackQuery) {
	cl, _ := db.Instance.GetClone(c.Me.Id)
	status := "Not set"
	enabled := true
	if cl != nil {
		enabled = cl.LoggerEnabled
		if cl.LoggerID != 0 {
			status = fmt.Sprintf("<code>%d</code>", cl.LoggerID)
		}
	}
	toggleLabel := "🔕 Disable"
	if !enabled {
		toggleLabel = "🔔 Enable"
	}
	text := fmt.Sprintf("<b>📋 Logger Settings</b>\n\nChannel: %s\nEnabled: %v", status, enabled)
	_, _ = cb.EditMessageText(c, text, &td.EditTextMessageOpts{
		ParseMode: "HTML",
		ReplyMarkup: &td.ReplyMarkupInlineKeyboard{Rows: [][]td.InlineKeyboardButton{
			{cloneBtn("Set Channel", "cedit_logger_set"), cloneBtn(toggleLabel, "cedit_logger_toggle")},
			{cloneBtn("Remove", "cedit_logger_remove")},
			{cloneBtn("« Back", "cedit_back")},
		}},
	})
}

func showImageMenu(c *td.Client, cb *td.UpdateNewCallbackQuery) {
	cl, _ := db.Instance.GetClone(c.Me.Id)
	status := "Not set"
	if cl != nil && cl.StartImg != "" {
		status = "Set ✅"
	}
	text := fmt.Sprintf("<b>🖼 Start Image</b>\n\nStatus: %s", status)
	_, _ = cb.EditMessageText(c, text, &td.EditTextMessageOpts{
		ParseMode: "HTML",
		ReplyMarkup: &td.ReplyMarkupInlineKeyboard{Rows: [][]td.InlineKeyboardButton{
			{cloneBtn("Set Image", "cedit_img_set"), cloneBtn("Remove", "cedit_img_remove")},
			{cloneBtn("« Back", "cedit_back")},
		}},
	})
}

func showButtonMenu(c *td.Client, cb *td.UpdateNewCallbackQuery) {
	cl, _ := db.Instance.GetClone(c.Me.Id)
	var rows [][]td.InlineKeyboardButton
	text := "<b>🔘 Custom /start Buttons</b>\n\n"
	if cl == nil || len(cl.CustomButtons) == 0 {
		text += "<i>No custom buttons yet.</i>"
	} else {
		for i, b := range cl.CustomButtons {
			text += fmt.Sprintf("%d. %s → %s\n", i+1, b.Text, b.Url)
			rows = append(rows, []td.InlineKeyboardButton{
				cloneBtn(fmt.Sprintf("❌ Remove #%d", i+1), fmt.Sprintf("cedit_btn_rm_%d", i)),
			})
		}
	}
	if cl == nil || len(cl.CustomButtons) < 5 {
		rows = append(rows, []td.InlineKeyboardButton{cloneBtn("➕ Add Button", "cedit_btn_add")})
	}
	rows = append(rows, []td.InlineKeyboardButton{cloneBtn("« Back", "cedit_back")})

	_, _ = cb.EditMessageText(c, text, &td.EditTextMessageOpts{
		ParseMode:             "HTML",
		DisableWebPagePreview: true,
		ReplyMarkup:           &td.ReplyMarkupInlineKeyboard{Rows: rows},
	})
}

// editInputHandler captures the next text/photo message for a clone owner
// mid-way through an /edit flow. Register alongside editCmdHandler.
func editInputHandler(c *td.Client, m *td.Message) error {
	if !m.IsPrivate() {
		return nil
	}
	userID := m.SenderID()
	step := getEditState(userID)
	if step == editStepNone {
		return nil
	}
	if !canEdit(c, m) {
		return nil
	}

	switch step {
	case editStepLoggerID:
		text := strings.TrimSpace(m.GetText())
		var loggerID int64
		if fwd := m.ForwardInfo; fwd != nil {
			if origin, ok := fwd.Origin.(*td.MessageOriginChannel); ok {
				loggerID = origin.ChatId
			}
		}
		if loggerID == 0 {
			id, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				_, _ = m.ReplyText(c, "That doesn't look like a valid chat ID. Please try again, or /edit to cancel.", nil)
				return td.EndGroups
			}
			loggerID = id
		}

		// Verify the bot can actually reach this chat before saving —
		// otherwise every future log attempt fails silently in the
		// background instead of telling the owner right away.
		if _, err := c.SendTextMessage(loggerID, fmt.Sprintf("✅ Logger activated for @%s.", c.Me.Usernames.EditableUsername), nil); err != nil {
			_, _ = m.ReplyText(c, fmt.Sprintf(
				"<b>Couldn't send a message to that chat.</b>\n\nMake sure:\n• The chat ID is correct\n• This bot (@%s) has been added to that chat\n• This bot can post there\n\nError: <code>%s</code>\n\nTry again, or /edit to cancel.",
				c.Me.Usernames.EditableUsername, err.Error(),
			), &td.SendTextMessageOpts{ParseMode: "HTML"})
			return td.EndGroups
		}

		_ = db.Instance.SetCloneLogger(c.Me.Id, loggerID)
		_ = db.Instance.SetCloneLoggerEnabled(c.Me.Id, true)
		setEditState(userID, editStepNone)
		_, _ = m.ReplyText(c, fmt.Sprintf("✅ Logger set to <code>%d</code>. Logging is now enabled.", loggerID), &td.SendTextMessageOpts{ParseMode: "HTML", ReplyMarkup: editMainMenu()})
		return td.EndGroups

	case editStepStartImg:
		url := strings.TrimSpace(m.GetText())
		if url == "" {
			_, _ = m.ReplyText(c, "Please send an image URL (photo uploads aren't stored directly — host it and send the link).", nil)
			return td.EndGroups
		}
		_ = db.Instance.SetCloneStartImg(c.Me.Id, url)
		setEditState(userID, editStepNone)
		_, _ = m.ReplyText(c, "Start image updated.", &td.SendTextMessageOpts{ReplyMarkup: editMainMenu()})
		return td.EndGroups

	case editStepAddButton:
		parts := strings.SplitN(m.GetText(), "|", 2)
		if len(parts) != 2 {
			_, _ = m.ReplyText(c, "Invalid format. Send: <code>Button Text | https://example.com</code>", &td.SendTextMessageOpts{ParseMode: "HTML"})
			return td.EndGroups
		}
		text := strings.TrimSpace(parts[0])
		url := strings.TrimSpace(parts[1])
		if text == "" || url == "" || !strings.HasPrefix(url, "http") {
			_, _ = m.ReplyText(c, "Invalid format. Send: <code>Button Text | https://example.com</code>", &td.SendTextMessageOpts{ParseMode: "HTML"})
			return td.EndGroups
		}
		ok, err := db.Instance.AddCustomButton(c.Me.Id, db.CustomButton{Text: text, Url: url, IsInline: true})
		setEditState(userID, editStepNone)
		if err != nil || !ok {
			_, _ = m.ReplyText(c, "Failed to add button (maximum of 5 reached).", &td.SendTextMessageOpts{ReplyMarkup: editMainMenu()})
			return td.EndGroups
		}
		_, _ = m.ReplyText(c, "Button added.", &td.SendTextMessageOpts{ReplyMarkup: editMainMenu()})
		return td.EndGroups
	}

	return nil
}
