# Changelog

## v1.0.0 — Initial Arc Release

### Changes

- **Rebranded** from Fallen to Team Arc; support links updated to
  https://t.me/arcchatz (chat) and https://t.me/ArcUpdates (channel).

- **`/start` command** — private DM and group text now match the tosu4 style,
  with a platform list and concise uptime display.

- **Setup Guide button** — added to both the private DM keyboard and the group
  `/start` keyboard; shows a 4-step group setup guide with Back / Close buttons.

- **Arc API YouTube downloader** (`src/core/dl/arc_api.go`)
  - YouTube audio and video downloads exclusively use the Arc API
    (`/youtube/v2/download` → `/youtube/jobStatus`) with the same
    job-queue retry logic as `_api.py`.
  - All other platforms (Spotify, Apple Music, SoundCloud, Deezer, JioSaavn,
    Tidal, MXPlayer, Twitch, Kick …) continue to use the original API gateway
    path unchanged.

- **DB channel cache** (`arcCache` inside `arc_api.go`)
  - Owns a **separate** MongoDB connection using `DB_URI` (mirrors `_api.py`'s
    `Cache` class which uses `CACHE_DB`). Falls back to `MONGO_URI` if `DB_URI`
    is not set.
  - Database: `arcapi`, collection: `medias` — documents keyed by
    `(track_id, is_video)` storing `message_id`.
  - On each YouTube download request the cache is checked first; if a message
    is found the file is streamed directly from the Telegram media channel via
    the `DlBot` client, exactly as `_api.py` does via `track.download()`.
  - `SaveToDBCache(videoID, video, msgID)` is exposed so the calling code can
    write back to the cache after uploading a new file to `MEDIA_CHANNEL_ID`.

- **New env vars**
  - `MEDIA_CHANNEL_ID` — Telegram channel ID holding cached media files.
  - `DB_URI` — separate MongoDB URI for the Arc media cache; falls back to
    `MONGO_URI` if unset.

- **Default DB name** changed to `ArcMusicBot`.
