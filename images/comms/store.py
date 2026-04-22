"""Message store with JSONL backend and read cursor tracking.

Each channel is a JSONL file under data_dir/channels/{name}.jsonl.
Channel metadata stored in data_dir/channels/{name}.meta.json.
Read cursors stored in data_dir/cursors/{participant}.json.
SQLite FTS5 index for full-text search.
"""

import json
import logging
import sqlite3
import uuid
from datetime import datetime, timezone
from pathlib import Path

from typing import Optional
from images.models.comms import Channel, ChannelState, ChannelType, Message, MessageFlags

log = logging.getLogger("agency.comms.store")


class MessageStore:
    def __init__(self, data_dir: Path):
        self.data_dir = data_dir
        self._channels_dir = data_dir / "channels"
        self._cursors_dir = data_dir / "cursors"
        self._channels_dir.mkdir(parents=True, exist_ok=True)
        self._cursors_dir.mkdir(parents=True, exist_ok=True)
        self._init_search_index()

    def _init_search_index(self) -> None:
        db_path = self.data_dir / "index.db"
        self._db = sqlite3.connect(str(db_path))
        self._db.execute("PRAGMA journal_mode=WAL")
        self._db.execute(
            "CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5("
            "id, channel, author, content, timestamp, tokenize='porter')"
        )
        self._db.commit()

    # -- Channel management --

    def create_channel(
        self,
        name: str,
        type: ChannelType,
        created_by: str,
        topic: str = "",
        members: Optional[list[str]] = None,
        visibility: str = "open",
    ) -> Channel:
        meta_path = self._channels_dir / f"{name}.meta.json"
        if meta_path.exists():
            raise ValueError(f"Channel {name!r} already exists")
        ch = Channel(
            name=name,
            type=type,
            created_by=created_by,
            topic=topic,
            members=members or [],
            visibility=visibility,
        )
        meta_path.write_text(ch.model_dump_json(indent=2))
        return ch

    def get_channel(self, name: str) -> Channel:
        meta_path = self._channels_dir / f"{name}.meta.json"
        if not meta_path.exists():
            raise ValueError(f"Channel {name!r} not found")
        return Channel.model_validate_json(meta_path.read_text())

    def list_channels(self, member: Optional[str] = None, state: Optional[str] = "active") -> list[Channel]:
        channels = []
        for meta_path in sorted(self._channels_dir.glob("*.meta.json")):
            ch = Channel.model_validate_json(meta_path.read_text())
            if state is not None and state != "all":
                if ch.state.value != state:
                    continue
            if member is not None and member not in ch.members:
                continue
            if ch.visibility == "private" and member is None:
                continue
            channels.append(ch)
        return channels

    def archive_channel(self, name: str, archived_by: str) -> Channel:
        meta_path = self._channels_dir / f"{name}.meta.json"
        if not meta_path.exists():
            raise ValueError(f"Channel {name!r} not found")
        ch = Channel.model_validate_json(meta_path.read_text())
        if ch.state == ChannelState.ARCHIVED:
            raise ValueError(f"Channel {name!r} is already archived")
        ch.state = ChannelState.ARCHIVED
        ch.archived_at = datetime.now(timezone.utc)
        ch.archived_by = archived_by
        meta_path.write_text(ch.model_dump_json(indent=2))
        return ch

    def retire_channel_name(self, name: str, retired_by: str) -> Channel:
        meta_path = self._channels_dir / f"{name}.meta.json"
        jsonl_path = self._channels_dir / f"{name}.jsonl"
        if not meta_path.exists():
            raise ValueError(f"Channel {name!r} not found")
        ch = Channel.model_validate_json(meta_path.read_text())
        if not ch.id:
            ch.id = str(uuid.uuid4())
        suffix = ch.id.replace("-", "")[:12]
        retired_name = f"{name}-deleted-{suffix}"
        retired_meta_path = self._channels_dir / f"{retired_name}.meta.json"
        retired_jsonl_path = self._channels_dir / f"{retired_name}.jsonl"
        if retired_meta_path.exists() or retired_jsonl_path.exists():
            raise ValueError(f"Retired channel name {retired_name!r} already exists")
        ch.name = retired_name
        ch.base_name = ch.base_name or name
        ch.state = ChannelState.ARCHIVED
        ch.archived_at = datetime.now(timezone.utc)
        ch.archived_by = retired_by
        retired_meta_path.write_text(ch.model_dump_json(indent=2))
        meta_path.unlink()
        if jsonl_path.exists():
            jsonl_path.rename(retired_jsonl_path)
        return ch

    def grant_channel_access(self, name: str, agent_name: str) -> Channel:
        meta_path = self._channels_dir / f"{name}.meta.json"
        if not meta_path.exists():
            raise ValueError(f"Channel {name!r} not found")
        ch = Channel.model_validate_json(meta_path.read_text())
        if agent_name not in ch.members:
            ch.members.append(agent_name)
            meta_path.write_text(ch.model_dump_json(indent=2))
        return ch

    def join_channel(self, channel_name: str, participant: str) -> None:
        ch = self.get_channel(channel_name)
        if ch.state == ChannelState.ARCHIVED:
            raise ValueError(f"Cannot join archived channel {channel_name!r}")
        if ch.visibility == "private":
            raise ValueError(f"Cannot join private channel {channel_name!r}")
        if participant not in ch.members:
            ch.members.append(participant)
            meta_path = self._channels_dir / f"{channel_name}.meta.json"
            meta_path.write_text(ch.model_dump_json(indent=2))

    def leave_channel(self, channel_name: str, participant: str) -> None:
        ch = self.get_channel(channel_name)
        if participant in ch.members:
            ch.members.remove(participant)
            meta_path = self._channels_dir / f"{channel_name}.meta.json"
            meta_path.write_text(ch.model_dump_json(indent=2))

    def leave_all_channels(self, participant: str) -> None:
        """Remove a participant from every channel they belong to."""
        for ch in self.list_channels(member=participant, state="all"):
            self.leave_channel(ch.name, participant)

    # -- Messages --

    def post_message(
        self,
        channel: str,
        author: str,
        content: str,
        reply_to: Optional[str] = None,
        flags: Optional[dict] = None,
        metadata: Optional[dict] = None,
    ) -> Message:
        ch = self.get_channel(channel)
        if ch.state == ChannelState.ARCHIVED:
            raise ValueError(f"Channel {channel!r} is archived (read-only)")
        if ch.visibility == "private" and author not in ch.members:
            raise ValueError(f"{author!r} is not a member of private channel {channel!r}")

        msg_flags = MessageFlags(**(flags or {}))
        msg = Message(
            channel=channel,
            author=author,
            content=content,
            reply_to=reply_to,
            flags=msg_flags,
            metadata=metadata or {},
        )

        # Append to JSONL
        jsonl_path = self._channels_dir / f"{channel}.jsonl"
        with open(jsonl_path, "a") as f:
            f.write(msg.model_dump_json() + "\n")

        # Search indexing and cursors are derived state. Message persistence is
        # the JSONL append above, so do not fail the send if these side effects
        # are temporarily unwritable.
        self._index_message(msg)

        self._set_cursor(channel, author, msg.timestamp)

        return msg

    def read_messages(
        self,
        channel: str,
        since: Optional[datetime] = None,
        limit: int = 50,
        reader: Optional[str] = None,
    ) -> list[Message]:
        ch = self.get_channel(channel)
        if ch.visibility == "private" and reader is not None and reader not in ch.members:
            raise ValueError(f"{reader!r} is not a member of private channel {channel!r}")

        jsonl_path = self._channels_dir / f"{channel}.jsonl"
        if not jsonl_path.exists():
            return []

        messages = []
        with open(jsonl_path) as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                msg = Message.model_validate_json(line)
                if msg.deleted:
                    continue
                if since and msg.timestamp <= since:
                    continue
                messages.append(msg)
                # Keep only the last `limit` messages in memory
                if not since and len(messages) > limit * 2:
                    messages = messages[-limit:]

        if len(messages) > limit:
            messages = messages[-limit:]

        if reader and messages:
            latest = max(m.timestamp for m in messages)
            self._set_cursor(channel, reader, latest)

        return messages

    def edit_message(
        self,
        channel: str,
        message_id: str,
        new_content: str,
        author: str,
    ) -> Message:
        jsonl_path = self._channels_dir / f"{channel}.jsonl"
        if not jsonl_path.exists():
            raise ValueError(f"Message {message_id!r} not found in channel {channel!r}")

        lines = jsonl_path.read_text().strip().splitlines()
        target_idx = None
        messages = []
        for i, line in enumerate(lines):
            if not line.strip():
                continue
            msg = Message.model_validate_json(line)
            if msg.id == message_id:
                target_idx = i
            messages.append((i, msg))

        if target_idx is None:
            raise ValueError(f"Message {message_id!r} not found in channel {channel!r}")

        # Find the target message object
        target_msg = next(msg for i, msg in messages if i == target_idx)

        if target_msg.author != author:
            raise PermissionError(f"Only the author ({target_msg.author!r}) can edit this message")

        # Append old content to edit history
        target_msg.edit_history.append({
            "content": target_msg.content,
            "edited_at": (target_msg.edited_at or target_msg.timestamp).isoformat(),
        })
        target_msg.content = new_content
        target_msg.edited_at = datetime.now(timezone.utc)

        # Rewrite JSONL
        with open(jsonl_path, "w") as f:
            for _, msg in messages:
                f.write(msg.model_dump_json() + "\n")

        # Update FTS index
        self._db.execute(
            "UPDATE messages_fts SET content = ? WHERE id = ?",
            (new_content, message_id),
        )
        self._db.commit()

        return target_msg

    def delete_message(
        self,
        channel: str,
        message_id: str,
        author: str,
    ) -> Message:
        jsonl_path = self._channels_dir / f"{channel}.jsonl"
        if not jsonl_path.exists():
            raise ValueError(f"Message {message_id!r} not found in channel {channel!r}")

        lines = jsonl_path.read_text().strip().splitlines()
        target_idx = None
        messages = []
        for i, line in enumerate(lines):
            if not line.strip():
                continue
            msg = Message.model_validate_json(line)
            if msg.id == message_id:
                target_idx = i
            messages.append((i, msg))

        if target_idx is None:
            raise ValueError(f"Message {message_id!r} not found in channel {channel!r}")

        target_msg = next(msg for i, msg in messages if i == target_idx)

        if target_msg.author != author:
            raise PermissionError(f"Only the author ({target_msg.author!r}) can delete this message")

        target_msg.deleted = True
        target_msg.content = ""

        # Rewrite JSONL
        with open(jsonl_path, "w") as f:
            for _, msg in messages:
                f.write(msg.model_dump_json() + "\n")

        # Remove from FTS index
        self._db.execute("DELETE FROM messages_fts WHERE id = ?", (message_id,))
        self._db.commit()

        return target_msg

    # -- Reactions --

    def add_reaction(
        self,
        channel: str,
        message_id: str,
        emoji: str,
        author: str,
    ) -> Message:
        jsonl_path = self._channels_dir / f"{channel}.jsonl"
        if not jsonl_path.exists():
            raise ValueError(f"Message {message_id!r} not found in channel {channel!r}")

        lines = jsonl_path.read_text().strip().splitlines()
        target_idx = None
        messages = []
        for i, line in enumerate(lines):
            if not line.strip():
                continue
            msg = Message.model_validate_json(line)
            if msg.id == message_id:
                target_idx = i
            messages.append((i, msg))

        if target_idx is None:
            raise ValueError(f"Message {message_id!r} not found in channel {channel!r}")

        target_msg = next(msg for i, msg in messages if i == target_idx)

        # Check for duplicate (same emoji + author) — silently no-op
        for reaction in target_msg.reactions:
            if reaction.get("emoji") == emoji and reaction.get("author") == author:
                return target_msg

        target_msg.reactions.append({"emoji": emoji, "author": author})

        # Rewrite JSONL
        with open(jsonl_path, "w") as f:
            for _, msg in messages:
                f.write(msg.model_dump_json() + "\n")

        return target_msg

    def remove_reaction(
        self,
        channel: str,
        message_id: str,
        emoji: str,
        author: str,
    ) -> Message:
        jsonl_path = self._channels_dir / f"{channel}.jsonl"
        if not jsonl_path.exists():
            raise ValueError(f"Message {message_id!r} not found in channel {channel!r}")

        lines = jsonl_path.read_text().strip().splitlines()
        target_idx = None
        messages = []
        for i, line in enumerate(lines):
            if not line.strip():
                continue
            msg = Message.model_validate_json(line)
            if msg.id == message_id:
                target_idx = i
            messages.append((i, msg))

        if target_idx is None:
            raise ValueError(f"Message {message_id!r} not found in channel {channel!r}")

        target_msg = next(msg for i, msg in messages if i == target_idx)

        target_msg.reactions = [
            r for r in target_msg.reactions
            if not (r.get("emoji") == emoji and r.get("author") == author)
        ]

        # Rewrite JSONL
        with open(jsonl_path, "w") as f:
            for _, msg in messages:
                f.write(msg.model_dump_json() + "\n")

        return target_msg

    # -- Search --

    def _index_message(self, msg: Message) -> None:
        try:
            self._db.execute(
                "INSERT INTO messages_fts (id, channel, author, content, timestamp) "
                "VALUES (?, ?, ?, ?, ?)",
                (msg.id, msg.channel, msg.author, msg.content, msg.timestamp.isoformat()),
            )
            self._db.commit()
        except Exception as exc:
            log.warning("message search indexing failed", extra={"error": str(exc), "channel": msg.channel})

    def search_messages(
        self,
        query: str,
        channel: Optional[str] = None,
        author: Optional[str] = None,
        participant: Optional[str] = None,
    ) -> list[Message]:
        conditions = ["messages_fts MATCH ?"]
        params: list[str] = [query]

        if channel:
            conditions.append("channel = ?")
            params.append(channel)
        if author:
            conditions.append("author = ?")
            params.append(author)

        where = " AND ".join(conditions)
        sql = (
            "SELECT id, channel, author, content, timestamp "
            f"FROM messages_fts WHERE {where} ORDER BY rank"
        )
        rows = self._db.execute(sql, params).fetchall()

        visible_channels = None
        if participant:
            visible_channels = {c.name for c in self.list_channels(member=participant)}

        results = []
        for row in rows:
            msg_channel = row[1]
            if visible_channels is not None and msg_channel not in visible_channels:
                continue
            results.append(Message(
                id=row[0],
                channel=msg_channel,
                author=row[2],
                content=row[3],
                timestamp=datetime.fromisoformat(row[4]),
            ))

        return results

    # -- Unread tracking --

    def get_unreads(self, participant: str) -> dict:
        channels = self.list_channels(member=participant)
        mention_tag = f"@{participant}"
        result = {}
        for ch in channels:
            cursor = self._get_cursor(ch.name, participant)
            jsonl_path = self._channels_dir / f"{ch.name}.jsonl"

            unread = 0
            mentions = 0

            if jsonl_path.exists():
                with open(jsonl_path) as f:
                    for line in f:
                        line = line.strip()
                        if not line:
                            continue
                        # Use raw JSON parse instead of full Pydantic validation
                        raw = json.loads(line)
                        if raw.get("author") == participant:
                            continue
                        ts = datetime.fromisoformat(raw["timestamp"])
                        if cursor is None or ts > cursor:
                            unread += 1
                            if mention_tag in raw.get("content", ""):
                                mentions += 1

            result[ch.name] = {
                "unread": unread,
                "mentions": mentions,
            }
        return result

    def mark_read(self, channel: str, participant: str) -> None:
        jsonl_path = self._channels_dir / f"{channel}.jsonl"
        if not jsonl_path.exists():
            return
        lines = jsonl_path.read_text().strip().splitlines()
        if not lines:
            return
        last_msg = Message.model_validate_json(lines[-1])
        self._set_cursor(channel, participant, last_msg.timestamp)

    def reset_cursors(self, participant: str, before: datetime) -> None:
        """Roll back all channel cursors for participant to before.

        Only moves cursors backward — if the current cursor is already older
        than before, it is left unchanged. Used on session restart to ensure
        messages posted recently are not silently missed.
        """
        cursor_path = self._cursors_dir / f"{participant}.json"
        if not cursor_path.exists():
            return
        cursors = json.loads(cursor_path.read_text())
        changed = False
        for channel, ts_str in list(cursors.items()):
            if datetime.fromisoformat(ts_str) > before:
                cursors[channel] = before.isoformat()
                changed = True
        if changed:
            cursor_path.write_text(json.dumps(cursors, indent=2))

    # -- Cursor helpers --

    def _get_cursor(self, channel: str, participant: str) -> Optional[datetime]:
        cursor_path = self._cursors_dir / f"{participant}.json"
        if not cursor_path.exists():
            return None
        cursors = json.loads(cursor_path.read_text())
        ts = cursors.get(channel)
        if ts is None:
            return None
        return datetime.fromisoformat(ts)

    def _set_cursor(self, channel: str, participant: str, ts: datetime) -> None:
        try:
            cursor_path = self._cursors_dir / f"{participant}.json"
            cursors = {}
            if cursor_path.exists():
                cursors = json.loads(cursor_path.read_text())
            current = cursors.get(channel)
            if current is None or datetime.fromisoformat(current) < ts:
                cursors[channel] = ts.isoformat()
                cursor_path.write_text(json.dumps(cursors, indent=2))
        except Exception as exc:
            log.warning("read cursor update failed", extra={"error": str(exc), "channel": channel, "participant": participant})

    # -- Write buffer (cache mode) --

    def buffer_message(self, channel, author, content, reply_to=None, flags=None, timestamp=None):
        """Buffer a message locally when upstream is unavailable."""
        import uuid
        buffer_dir = self.data_dir / "buffer"
        buffer_dir.mkdir(parents=True, exist_ok=True)
        local_id = f"local-{uuid.uuid4().hex[:12]}"
        ts = timestamp or datetime.now(timezone.utc).isoformat()
        entry = {
            "id": local_id, "channel": channel, "author": author,
            "content": content, "timestamp": ts,
            "reply_to": reply_to, "flags": flags or {},
        }
        buffer_path = buffer_dir / f"{channel}.jsonl"
        with open(buffer_path, "a") as f:
            f.write(json.dumps(entry) + "\n")
        return entry

    def read_buffer(self, channel):
        """Read all buffered messages for a channel in FIFO order."""
        buffer_path = self.data_dir / "buffer" / f"{channel}.jsonl"
        if not buffer_path.exists():
            return []
        entries = []
        for line in buffer_path.read_text().strip().splitlines():
            if line.strip():
                entries.append(json.loads(line))
        return entries

    def remove_buffer_entry(self, channel, entry_id):
        """Remove a specific entry from the buffer (after successful drain)."""
        buffer_path = self.data_dir / "buffer" / f"{channel}.jsonl"
        if not buffer_path.exists():
            return
        lines = buffer_path.read_text().strip().splitlines()
        remaining = []
        for line in lines:
            if line.strip():
                entry = json.loads(line)
                if entry.get("id") != entry_id:
                    remaining.append(line)
        if remaining:
            buffer_path.write_text("\n".join(remaining) + "\n")
        else:
            buffer_path.unlink(missing_ok=True)

    def buffer_channels(self):
        """List channels that have buffered messages."""
        buffer_dir = self.data_dir / "buffer"
        if not buffer_dir.exists():
            return []
        return [p.stem for p in buffer_dir.glob("*.jsonl") if p.stat().st_size > 0]

    def buffer_size(self, channel):
        """Count buffered messages for a channel."""
        return len(self.read_buffer(channel))

    # -- ID remap table (cache mode) --

    def _remap_path(self):
        return self.data_dir / "buffer" / "id-remap.json"

    def _load_remap(self):
        path = self._remap_path()
        if not path.exists():
            return {}
        return json.loads(path.read_text())

    def _save_remap(self, remap):
        path = self._remap_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(remap))

    def add_id_remap(self, local_id, server_id):
        """Record a local->server ID mapping after buffer drain."""
        remap = self._load_remap()
        remap[local_id] = server_id
        self._save_remap(remap)

    def resolve_id(self, msg_id):
        """Resolve a message ID through the remap table. Pass-through if not found."""
        remap = self._load_remap()
        return remap.get(msg_id, msg_id)

    def clear_id_remap(self, local_ids):
        """Remove resolved entries from the remap table."""
        remap = self._load_remap()
        for lid in local_ids:
            remap.pop(lid, None)
        self._save_remap(remap)
