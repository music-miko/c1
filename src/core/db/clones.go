/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package db

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// CustomButton is a single user-configured inline button shown under a
// clone's /start message (max 5 per clone, mirrors the Python schema).
type CustomButton struct {
	Text   string `bson:"text"`
	Url    string `bson:"url"`
	Inline bool   `bson:"inline"`
	Style  string `bson:"style"`
}

// Clone represents a single clone-bot registration.
// _id is the Telegram bot user ID (numeric part of the token).
type Clone struct {
	ID            int64          `bson:"_id"`
	Token         string         `bson:"token"`
	OwnerID       int64          `bson:"owner_id"`
	Username      string         `bson:"username"`
	Name          string         `bson:"name"`
	Active        bool           `bson:"active"`
	CreatedAt     int64          `bson:"created_at"`
	LoggerID      int64          `bson:"logger_id"`
	LoggerEnabled bool           `bson:"logger_enabled"`
	StartImg      string         `bson:"start_img"`
	CustomButtons []CustomButton `bson:"custom_buttons"`
	SudoUsers     []int64        `bson:"sudo_users"`
}

// RegisterClone upserts a clone registration and marks it active.
func (db *Database) RegisterClone(botID, ownerID int64, token, username, name string) error {
	ctx, cancel := db.ctx()
	defer cancel()

	_, err := db.cloneDB.UpdateOne(ctx,
		bson.M{"_id": botID},
		bson.M{
			"$set": bson.M{
				"token":      token,
				"owner_id":   ownerID,
				"username":   username,
				"name":       name,
				"active":     true,
				"created_at": time.Now().Unix(),
			},
			"$setOnInsert": bson.M{
				"logger_enabled": true,
				"custom_buttons": []CustomButton{},
				"sudo_users":     []int64{},
			},
		},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

// GetClone fetches a single clone by its bot ID.
func (db *Database) GetClone(botID int64) (*Clone, error) {
	ctx, cancel := db.ctx()
	defer cancel()

	var c Clone
	err := db.cloneDB.FindOne(ctx, bson.M{"_id": botID}).Decode(&c)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &c, nil
}

// GetCloneByOwner returns the (first) clone owned by the given user, if any.
func (db *Database) GetCloneByOwner(ownerID int64) (*Clone, error) {
	ctx, cancel := db.ctx()
	defer cancel()

	var c Clone
	err := db.cloneDB.FindOne(ctx, bson.M{"owner_id": ownerID}).Decode(&c)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &c, nil
}

// GetAllActiveClones returns every clone currently marked active (used to
// restore clones on process boot).
func (db *Database) GetAllActiveClones() ([]*Clone, error) {
	return db.queryClones(bson.M{"active": true})
}

// GetAllClones returns every clone registration, active or not.
func (db *Database) GetAllClones() ([]*Clone, error) {
	return db.queryClones(bson.M{})
}

func (db *Database) queryClones(filter bson.M) ([]*Clone, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cursor, err := db.cloneDB.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer func(cursor *mongo.Cursor, ctx context.Context) {
		_ = cursor.Close(ctx)
	}(cursor, ctx)

	var clones []*Clone
	for cursor.Next(ctx) {
		var c Clone
		if err := cursor.Decode(&c); err != nil {
			return nil, err
		}
		clones = append(clones, &c)
	}
	return clones, cursor.Err()
}

// DeactivateClone marks a clone inactive without deleting its data.
func (db *Database) DeactivateClone(botID int64) error {
	ctx, cancel := db.ctx()
	defer cancel()

	_, err := db.cloneDB.UpdateOne(ctx,
		bson.M{"_id": botID},
		bson.M{"$set": bson.M{"active": false}},
	)
	return err
}

// DeleteCloneAllData permanently removes the clone's registry entry.
func (db *Database) DeleteCloneAllData(botID int64) error {
	ctx, cancel := db.ctx()
	defer cancel()

	_, err := db.cloneDB.DeleteOne(ctx, bson.M{"_id": botID})
	return err
}

// CountClones returns (total, active) clone counts.
func (db *Database) CountClones() (int64, int64, error) {
	ctx, cancel := db.ctx()
	defer cancel()

	total, err := db.cloneDB.CountDocuments(ctx, bson.M{})
	if err != nil {
		return 0, 0, err
	}
	active, err := db.cloneDB.CountDocuments(ctx, bson.M{"active": true})
	if err != nil {
		return total, 0, err
	}
	return total, active, nil
}

// --- /edit settings -------------------------------------------------------

// SetCloneLogger sets the log-channel ID for a clone bot.
func (db *Database) SetCloneLogger(botID, loggerID int64) error {
	ctx, cancel := db.ctx()
	defer cancel()
	_, err := db.cloneDB.UpdateOne(ctx, bson.M{"_id": botID}, bson.M{"$set": bson.M{"logger_id": loggerID}})
	return err
}

// GetCloneLogger returns the configured log-channel ID for a clone, or 0.
func (db *Database) GetCloneLogger(botID int64) (int64, error) {
	c, err := db.GetClone(botID)
	if err != nil || c == nil {
		return 0, err
	}
	return c.LoggerID, nil
}

// SetCloneLoggerEnabled toggles whether the logger is active for a clone.
func (db *Database) SetCloneLoggerEnabled(botID int64, enabled bool) error {
	ctx, cancel := db.ctx()
	defer cancel()
	_, err := db.cloneDB.UpdateOne(ctx, bson.M{"_id": botID}, bson.M{"$set": bson.M{"logger_enabled": enabled}})
	return err
}

// GetCloneLoggerEnabled reports whether the logger is active for a clone
// (defaults to true if unset, matching the Python behaviour).
func (db *Database) GetCloneLoggerEnabled(botID int64) (bool, error) {
	c, err := db.GetClone(botID)
	if err != nil || c == nil {
		return true, err
	}
	return c.LoggerEnabled, nil
}

// SetCloneStartImg sets (or clears, with "") the /start image for a clone.
func (db *Database) SetCloneStartImg(botID int64, url string) error {
	ctx, cancel := db.ctx()
	defer cancel()
	_, err := db.cloneDB.UpdateOne(ctx, bson.M{"_id": botID}, bson.M{"$set": bson.M{"start_img": url}})
	return err
}

// AddCustomButton appends a custom /start button (max 5). Returns false if
// the clone already has 5 buttons.
func (db *Database) AddCustomButton(botID int64, btn CustomButton) (bool, error) {
	c, err := db.GetClone(botID)
	if err != nil {
		return false, err
	}
	if c == nil {
		return false, errors.New("clone not found")
	}
	if len(c.CustomButtons) >= 5 {
		return false, nil
	}

	ctx, cancel := db.ctx()
	defer cancel()
	_, err = db.cloneDB.UpdateOne(ctx, bson.M{"_id": botID}, bson.M{"$push": bson.M{"custom_buttons": btn}})
	return err == nil, err
}

// RemoveCustomButton removes the button at the given 0-based index.
func (db *Database) RemoveCustomButton(botID int64, index int) error {
	c, err := db.GetClone(botID)
	if err != nil || c == nil {
		return err
	}
	if index < 0 || index >= len(c.CustomButtons) {
		return errors.New("button index out of range")
	}
	buttons := append(c.CustomButtons[:index], c.CustomButtons[index+1:]...)

	ctx, cancel := db.ctx()
	defer cancel()
	_, err = db.cloneDB.UpdateOne(ctx, bson.M{"_id": botID}, bson.M{"$set": bson.M{"custom_buttons": buttons}})
	return err
}

// AddCloneChat records that a clone bot is present in chatID, used to scope
// that clone's own /broadcast to groups it actually runs in.
func (db *Database) AddCloneChat(botID, chatID int64) error {
	ctx, cancel := db.ctx()
	defer cancel()
	_, err := db.cloneDB.UpdateOne(ctx, bson.M{"_id": botID}, bson.M{"$addToSet": bson.M{"chats": chatID}})
	return err
}

// RemoveCloneChat forgets a chat for a clone bot (e.g. after being kicked).
func (db *Database) RemoveCloneChat(botID, chatID int64) error {
	ctx, cancel := db.ctx()
	defer cancel()
	_, err := db.cloneDB.UpdateOne(ctx, bson.M{"_id": botID}, bson.M{"$pull": bson.M{"chats": chatID}})
	return err
}

// GetCloneChats returns every chat a clone bot is currently known to be in.
func (db *Database) GetCloneChats(botID int64) ([]int64, error) {
	ctx, cancel := db.ctx()
	defer cancel()

	var doc struct {
		Chats []int64 `bson:"chats"`
	}
	err := db.cloneDB.FindOne(ctx, bson.M{"_id": botID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return doc.Chats, nil
}

// --- clone sudo users -------------------------------------------------------

// AddCloneSudo grants a user sudo rights on a specific clone.
func (db *Database) AddCloneSudo(botID, userID int64) error {
	ctx, cancel := db.ctx()
	defer cancel()
	_, err := db.cloneDB.UpdateOne(ctx, bson.M{"_id": botID}, bson.M{"$addToSet": bson.M{"sudo_users": userID}})
	return err
}

// RemoveCloneSudo revokes a user's sudo rights on a specific clone.
func (db *Database) RemoveCloneSudo(botID, userID int64) error {
	ctx, cancel := db.ctx()
	defer cancel()
	_, err := db.cloneDB.UpdateOne(ctx, bson.M{"_id": botID}, bson.M{"$pull": bson.M{"sudo_users": userID}})
	return err
}

// IsCloneSudo reports whether userID is the owner or a sudo user of the clone.
func (db *Database) IsCloneSudo(botID, userID int64) (bool, error) {
	c, err := db.GetClone(botID)
	if err != nil || c == nil {
		return false, err
	}
	if c.OwnerID == userID {
		return true, nil
	}
	for _, id := range c.SudoUsers {
		if id == userID {
			return true, nil
		}
	}
	return false, nil
}
