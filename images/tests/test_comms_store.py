"""Tests for comms message store (JSONL backend + read cursors + search)."""

import json

import pytest

from images.comms.store import MessageStore
from images.models.comms import ChannelType, Message


@pytest.fixture
def store(tmp_path):
    return MessageStore(data_dir=tmp_path)


class TestChannelManagement:
    def test_create_channel(self, store):
        ch = store.create_channel("test", ChannelType.TEAM, "operator", topic="Test")
        assert ch.name == "test"
        assert ch.type == ChannelType.TEAM

    def test_create_duplicate_channel_raises(self, store):
        store.create_channel("test", ChannelType.TEAM, "operator")
        with pytest.raises(ValueError, match="already exists"):
            store.create_channel("test", ChannelType.TEAM, "operator")

    def test_retire_channel_name_frees_alias_and_preserves_id(self, store):
        first = store.create_channel(
            "dm-henry",
            ChannelType.DIRECT,
            "_platform",
            members=["henry", "_operator"],
            visibility="private",
        )
        store.post_message("dm-henry", "henry", "old history")

        retired = store.retire_channel_name("dm-henry", "_platform")

        assert retired.id == first.id
        assert retired.name.startswith("dm-henry-deleted-")
        assert retired.base_name == "dm-henry"
        assert (store.data_dir / "channels" / f"{retired.name}.meta.json").exists()
        assert (store.data_dir / "channels" / f"{retired.name}.jsonl").exists()
        with pytest.raises(ValueError, match="not found"):
            store.get_channel("dm-henry")

        second = store.create_channel(
            "dm-henry",
            ChannelType.DIRECT,
            "_platform",
            members=["henry", "_operator"],
            visibility="private",
        )
        assert second.id != first.id
        assert second.name == "dm-henry"

    def test_list_channels(self, store):
        store.create_channel("alpha", ChannelType.TEAM, "operator")
        store.create_channel("beta", ChannelType.TEAM, "operator")
        channels = store.list_channels()
        names = [c.name for c in channels]
        assert "alpha" in names
        assert "beta" in names

    def test_list_channels_for_member(self, store):
        store.create_channel("public", ChannelType.TEAM, "operator", members=["scout", "pm"])
        store.create_channel("private", ChannelType.TEAM, "operator", members=["pm"])
        channels = store.list_channels(member="scout")
        assert len(channels) == 1
        assert channels[0].name == "public"

    def test_join_channel(self, store):
        store.create_channel("test", ChannelType.TEAM, "operator")
        store.join_channel("test", "scout")
        ch = store.get_channel("test")
        assert "scout" in ch.members

    def test_join_already_member(self, store):
        store.create_channel("test", ChannelType.TEAM, "operator", members=["scout"])
        store.join_channel("test", "scout")
        ch = store.get_channel("test")
        assert ch.members.count("scout") == 1

    def test_get_nonexistent_channel_raises(self, store):
        with pytest.raises(ValueError, match="not found"):
            store.get_channel("nope")


class TestMessages:
    def test_post_message(self, store):
        store.create_channel("test", ChannelType.TEAM, "operator", members=["scout"])
        msg = store.post_message("test", "scout", "Hello team")
        assert msg.channel == "test"
        assert msg.author == "scout"
        assert msg.content == "Hello team"

    def test_post_message_persisted_as_jsonl(self, store):
        store.create_channel("test", ChannelType.TEAM, "operator", members=["scout"])
        store.post_message("test", "scout", "First")
        store.post_message("test", "pm", "Second")
        jsonl_path = store.data_dir / "channels" / "test.jsonl"
        assert jsonl_path.exists()
        lines = jsonl_path.read_text().strip().splitlines()
        assert len(lines) == 2
        first = json.loads(lines[0])
        assert first["content"] == "First"

    def test_post_to_nonexistent_channel_raises(self, store):
        with pytest.raises(ValueError, match="not found"):
            store.post_message("nope", "scout", "Hello")

    def test_read_messages(self, store):
        store.create_channel("test", ChannelType.TEAM, "operator", members=["scout"])
        store.post_message("test", "scout", "One")
        store.post_message("test", "pm", "Two")
        store.post_message("test", "scout", "Three")
        msgs = store.read_messages("test")
        assert len(msgs) == 3
        assert msgs[0].content == "One"
        assert msgs[2].content == "Three"

    def test_read_messages_with_limit(self, store):
        store.create_channel("test", ChannelType.TEAM, "operator", members=["scout"])
        for i in range(10):
            store.post_message("test", "scout", f"Msg {i}")
        msgs = store.read_messages("test", limit=3)
        assert len(msgs) == 3
        assert msgs[0].content == "Msg 7"

    def test_read_messages_since(self, store):
        store.create_channel("test", ChannelType.TEAM, "operator", members=["scout"])
        m1 = store.post_message("test", "scout", "Before")
        m2 = store.post_message("test", "pm", "After")
        msgs = store.read_messages("test", since=m1.timestamp)
        assert len(msgs) == 1
        assert msgs[0].content == "After"

    def test_read_messages_survives_unwritable_cursor(self, store):
        store.create_channel("test", ChannelType.TEAM, "operator", members=["scout", "pm"])
        store.post_message("test", "pm", "Readable")
        cursor_path = store.data_dir / "cursors" / "scout.json"
        cursor_path.write_text("{}")
        cursor_path.chmod(0o400)
        try:
            msgs = store.read_messages("test", reader="scout")
        finally:
            cursor_path.chmod(0o600)
        assert [m.content for m in msgs] == ["Readable"]

    def test_post_message_survives_unwritable_search_index(self, store):
        store.create_channel("test", ChannelType.TEAM, "operator", members=["scout"])
        store._db.close()
        msg = store.post_message("test", "scout", "Persist even when search is down")
        assert msg.content == "Persist even when search is down"
        jsonl_path = store.data_dir / "channels" / "test.jsonl"
        assert "Persist even when search is down" in jsonl_path.read_text()

    def test_post_with_reply_to(self, store):
        store.create_channel("test", ChannelType.TEAM, "operator", members=["scout"])
        parent = store.post_message("test", "scout", "Question?")
        reply = store.post_message("test", "pm", "Answer.", reply_to=parent.id)
        assert reply.reply_to == parent.id

    def test_post_with_flags(self, store):
        store.create_channel("test", ChannelType.TEAM, "operator", members=["scout"])
        msg = store.post_message(
            "test", "pm", "Launch API first.",
            flags={"decision": True},
        )
        assert msg.flags.decision is True


class TestUnreadTracking:
    def test_initial_unread_count_is_zero(self, store):
        store.create_channel("test", ChannelType.TEAM, "operator", members=["scout"])
        unreads = store.get_unreads("scout")
        assert unreads["test"]["unread"] == 0

    def test_unread_count_after_post(self, store):
        store.create_channel("test", ChannelType.TEAM, "operator", members=["scout", "pm"])
        store.post_message("test", "pm", "Hey scout")
        unreads = store.get_unreads("scout")
        assert unreads["test"]["unread"] == 1

    def test_read_advances_cursor(self, store):
        store.create_channel("test", ChannelType.TEAM, "operator", members=["scout", "pm"])
        store.post_message("test", "pm", "Hey")
        store.read_messages("test", reader="scout")
        unreads = store.get_unreads("scout")
        assert unreads["test"]["unread"] == 0

    def test_mention_count(self, store):
        store.create_channel("test", ChannelType.TEAM, "operator", members=["scout", "pm"])
        store.post_message("test", "pm", "Hey @scout check this out")
        unreads = store.get_unreads("scout")
        assert unreads["test"]["mentions"] == 1

    def test_own_messages_dont_count_as_unread(self, store):
        store.create_channel("test", ChannelType.TEAM, "operator", members=["scout"])
        store.post_message("test", "scout", "Talking to myself")
        unreads = store.get_unreads("scout")
        assert unreads["test"]["unread"] == 0

    def test_mark_read(self, store):
        store.create_channel("test", ChannelType.TEAM, "operator", members=["scout", "pm"])
        store.post_message("test", "pm", "One")
        store.post_message("test", "pm", "Two")
        store.mark_read("test", "scout")
        unreads = store.get_unreads("scout")
        assert unreads["test"]["unread"] == 0


class TestSearch:
    @pytest.fixture(autouse=True)
    def setup_channels(self, store):
        store.create_channel("dev", ChannelType.TEAM, "operator", members=["scout", "pm"])
        store.create_channel("product", ChannelType.TEAM, "operator", members=["pm"])
        store.post_message("dev", "scout", "The database schema needs Drizzle ORM migration")
        store.post_message("dev", "pm", "What about the recipe API integration?")
        store.post_message("dev", "scout", "ChefHubDB adapter uses the RecipeAdapter interface")
        store.post_message("product", "pm", "Pricing tiers: Free, Starter 29, Pro 99")
        store.post_message("product", "pm", "First customers are meal planning apps")

    def test_search_single_term(self, store):
        results = store.search_messages("recipe")
        # FTS5 with porter stemmer: "recipe" matches "recipe" in
        # "recipe API integration" but not "RecipeAdapter" (single token)
        assert len(results) >= 1
        assert any("recipe" in r.content.lower() for r in results)

    def test_search_phrase(self, store):
        results = store.search_messages("Drizzle ORM")
        assert len(results) == 1
        assert "Drizzle" in results[0].content

    def test_search_by_channel(self, store):
        results = store.search_messages("planning", channel="product")
        assert len(results) == 1
        assert results[0].channel == "product"

    def test_search_by_author(self, store):
        results = store.search_messages("adapter", author="scout")
        assert len(results) == 1
        assert results[0].author == "scout"

    def test_search_no_results(self, store):
        results = store.search_messages("kubernetes")
        assert len(results) == 0

    def test_search_respects_member_visibility(self, store):
        results = store.search_messages("pricing", participant="scout")
        assert len(results) == 0

    def test_search_returns_results_for_member(self, store):
        results = store.search_messages("pricing", participant="pm")
        assert len(results) == 1


class TestPrivateChannels:
    def test_list_channels_hides_private_from_non_members(self, store):
        store.create_channel("public", ChannelType.TEAM, "operator", members=["scout", "pm"])
        store.create_channel("secret", ChannelType.TEAM, "operator", members=["operator"], visibility="private")
        channels = store.list_channels(member="scout")
        names = [c.name for c in channels]
        assert "public" in names
        assert "secret" not in names

    def test_list_channels_shows_private_to_members(self, store):
        store.create_channel("secret", ChannelType.TEAM, "operator", members=["operator", "scout"], visibility="private")
        channels = store.list_channels(member="scout")
        names = [c.name for c in channels]
        assert "secret" in names

    def test_list_channels_no_member_hides_private(self, store):
        store.create_channel("public", ChannelType.TEAM, "operator", members=["scout"])
        store.create_channel("secret", ChannelType.TEAM, "operator", members=["operator"], visibility="private")
        channels = store.list_channels(member=None)
        names = [c.name for c in channels]
        assert "public" in names
        assert "secret" not in names

    def test_join_private_channel_blocked(self, store):
        store.create_channel("secret", ChannelType.TEAM, "operator", members=["operator"], visibility="private")
        with pytest.raises(ValueError, match="private"):
            store.join_channel("secret", "scout")

    def test_post_to_private_channel_non_member_blocked(self, store):
        store.create_channel("secret", ChannelType.TEAM, "operator", members=["operator"], visibility="private")
        with pytest.raises(ValueError, match="not a member"):
            store.post_message("secret", "scout", "hello")

    def test_post_to_private_channel_member_allowed(self, store):
        store.create_channel("secret", ChannelType.TEAM, "operator", members=["operator"], visibility="private")
        msg = store.post_message("secret", "operator", "classified info")
        assert msg.content == "classified info"

    def test_read_private_channel_non_member_blocked(self, store):
        store.create_channel("secret", ChannelType.TEAM, "operator", members=["operator"], visibility="private")
        store.post_message("secret", "operator", "classified info")
        with pytest.raises(ValueError, match="not a member"):
            store.read_messages("secret", reader="scout")

    def test_read_private_channel_member_allowed(self, store):
        store.create_channel("secret", ChannelType.TEAM, "operator", members=["operator"], visibility="private")
        store.post_message("secret", "operator", "classified info")
        msgs = store.read_messages("secret", reader="operator")
        assert len(msgs) == 1


class TestEndToEnd:
    def test_full_comms_workflow(self, store):
        store.create_channel(
            "chefhub-beta", ChannelType.TEAM, "operator",
            topic="ChefHub beta readiness",
            members=["scout", "pm", "operator"],
        )

        msg1 = store.post_message(
            "chefhub-beta", "pm",
            "Recommend Option C: ChefHubDB as separate API product.",
            flags={"decision": True},
        )

        # Scout has 1 unread, PM has 0 (own message)
        assert store.get_unreads("scout")["chefhub-beta"]["unread"] == 1
        assert store.get_unreads("pm")["chefhub-beta"]["unread"] == 0

        # Scout reads
        msgs = store.read_messages("chefhub-beta", reader="scout")
        assert len(msgs) == 1
        assert msgs[0].flags.decision is True
        assert store.get_unreads("scout")["chefhub-beta"]["unread"] == 0

        # Scout replies
        store.post_message(
            "chefhub-beta", "scout",
            "Agreed. ChefHub already has RecipeAdapter pattern.",
            reply_to=msg1.id,
        )
        assert store.get_unreads("pm")["chefhub-beta"]["unread"] == 1

        # Operator @mentions both
        store.post_message(
            "chefhub-beta", "operator",
            "@scout @pm let's proceed with API-first launch.",
        )
        assert store.get_unreads("scout")["chefhub-beta"]["mentions"] == 1
        assert store.get_unreads("pm")["chefhub-beta"]["mentions"] == 1

    def test_multi_channel_visibility(self, store):
        store.create_channel("dev", ChannelType.TEAM, "operator", members=["scout", "pm"])
        store.create_channel("product", ChannelType.TEAM, "operator", members=["pm"])

        store.post_message("dev", "scout", "Database schema deployed")
        store.post_message("product", "pm", "Pricing: 29/99/299 tiers")

        # Search respects visibility
        assert len(store.search_messages("pricing", participant="scout")) == 0
        assert len(store.search_messages("pricing", participant="pm")) == 1
