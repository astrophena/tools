# © 2026 Ilya Mateyko. All rights reserved.
# Use of this source code is governed by the ISC
# license that can be found in the LICENSE.md file.

# vim: ft=starlark shiftwidth=4

"""Provides helpers for defining Telegram bots in Starlark.

This module is the higher-level companion to `@starlet//tg.star`. The `tg`
module is a convenience wrapper around Telegram Bot API calls; `tgbot` is a
small router for the repetitive parts of Telegram bot code:

  - reading user-visible text from either `message["text"]` or
    `message["caption"]`;
  - parsing only real Telegram `bot_command` entities at offset zero;
  - accepting `/command@this_bot` in groups while silently ignoring commands
    addressed to other bots;
  - registering commands with descriptions and argument policy;
  - publishing per-chat Telegram command menus during `on_load`;
  - deciding whether group messages should be handled, ignored, or treated as
    addressed to the bot;
  - enforcing a simple allowed-chat policy;
  - optionally forwarding unauthorized private messages to the owner.

Minimal bot:

    load("@starlet//tg.star", "tg")
    load("@starlet//tgbot.star", "tgbot")

    bot = tgbot.new(allowed_chats=[config.owner_id])

    def start(message, args):
        tg.reply(message, "Hello!")

    def echo(message, args):
        tg.reply(message, args)

    bot.command("/start", "Start the bot.", start, allow_unauthorized=True)
    bot.command("/echo", "Echo text.", echo, require_args=True, usage="/echo text")

    def fallback(message):
        tg.reply(message, "I only understand commands.")

    def handle(update):
        bot.handle(update, fallback=fallback)

    def on_load():
        bot.set_commands()

Typical group-aware bot:

    bot = tgbot.new(
        allowed_chats=[config.owner_id, -100123456789],
        group_mode="addressed_only",
        leave_unauthorized_groups=True,
        unauthorized_forward_to=config.owner_id,
        unauthorized_forward_ack="Forwarded to the owner.",
        unauthorized_command_texts={
            "/start": "Send a message and I will forward it to the owner.",
        },
        unknown_command_text="Unknown command.",
        empty_args_text="This command requires arguments.",
        usage_text="Usage: {}",
    )

With `group_mode="addressed_only"`, non-command group messages are handled only
when they mention `@{config.bot_username}` or reply to a message sent by the
bot. Commands still work directly from Telegram's command UI. For ordinary
private chats, every text or caption message is considered addressed to the bot.

`bot.handle` accepts three optional callbacks:

  - `before_message(message)`: runs after coarse filtering but before
    authorization and command dispatch. Use it for caching chat/user metadata
    only for messages on the bot's handling path.
  - `forwarded_handler(message) -> bool`: runs after authorization and before
    command dispatch. Return True to consume the forwarded message.
  - `fallback(message)`: runs for non-command messages that were not consumed
    by earlier policy or callbacks.

Command handlers receive `(message, args)`. `args` is the remaining text after
the leading command token, already stripped of surrounding whitespace. If a
command was registered with `require_args=True` and `args` is empty, the router
sends either `usage_text.format(usage)` or `empty_args_text` instead of calling
the command handler.

The standalone helpers exposed on `tgbot` (`text`, `parse_command`,
`is_group_chat`, `is_reply_to_bot`, and related functions) are useful when app
code needs the same Telegram semantics outside the router.
"""

load("@starlet//tg.star", "tg")


def _new(
    allowed_chats=None,
    group_mode="all",
    leave_unauthorized_groups=False,
    unauthorized_forward_to=None,
    unauthorized_forward_ack="",
    unauthorized_command_texts=None,
    unknown_command_text="Unknown command.",
    empty_args_text="This command requires arguments.",
    usage_text="Usage: {}",
):
    """Creates a Telegram bot router.

    The returned module stores command registrations in a private dictionary and
    exposes a small routing surface: register commands, handle updates, and
    publish Telegram command menus. Application code remains responsible for
    command bodies and any non-command fallback behavior.
    """
    if allowed_chats == None:
        allowed_chats = []
    if unauthorized_command_texts == None:
        unauthorized_command_texts = {}

    commands = {}

    def command(
        name,
        description,
        action,
        allow_unauthorized=False,
        require_args=False,
        usage="",
    ):
        """Registers one bot command.

        Commands are keyed by their slash form, for example `/start`. The
        router accepts Telegram's group form (`/start@botname`) while preserving
        whether the command was addressed to this bot.
        """
        if not name.startswith("/"):
            fail("command name must start with /")
        commands[name] = {
            "name": name,
            "description": description,
            "action": action,
            "allow_unauthorized": allow_unauthorized,
            "require_args": require_args,
            "usage": usage,
        }

    def handle(update, fallback=None, before_message=None, forwarded_handler=None):
        """Handles one Telegram update dictionary.

        Starlet passes raw Telegram updates to user code. This helper currently
        routes `message` updates and logs every other shape so new Telegram
        update types remain visible during development.
        """
        if "message" in update:
            return handle_message(
                update["message"],
                fallback=fallback,
                before_message=before_message,
                forwarded_handler=forwarded_handler,
            )

        print("unknown update: ", repr(update))

    def handle_message(
        message, fallback=None, before_message=None, forwarded_handler=None
    ):
        """Handles one Telegram message.

        The ordering here:
        1. leave unauthorized groups before doing any message-specific work;
        2. drop unsupported message shapes and ignored group chatter;
        3. let the application cache context for messages on the handling path;
        4. enforce private-chat authorization;
        5. allow application-specific forwarded-message handling;
        6. dispatch commands before falling back to normal message handling.
        """
        chat_id = tg.chat_id(message)

        if (
            leave_unauthorized_groups
            and _is_group_chat(message)
            and not is_allowed_chat(chat_id)
        ):
            print("left unauthorized group chat %s" % chat_id)
            tg.leave_chat(chat_id)
            return True

        if not _has_text(message):
            return False

        if should_ignore_group_message(message):
            return True

        if before_message != None:
            before_message(message)

        if handle_unauthorized_message(message):
            return True

        if forwarded_handler != None and _is_forwarded_message(message):
            if forwarded_handler(message):
                return True

        message = _strip_bot_mention(message, bot_username=config.bot_username)
        if dispatch_command(message):
            return True

        if fallback != None:
            fallback(message)
            return True

        return False

    def is_allowed_chat(chat_id):
        return chat_id in allowed_chats

    def should_ignore_group_message(message):
        """Returns whether a group message should be ignored by policy."""
        if group_mode == "all":
            return False
        if not _is_group_chat(message):
            return False
        if group_mode == "commands_only":
            return not _is_command(message, bot_username=config.bot_username)
        if group_mode == "addressed_only":
            if _is_command(message, bot_username=config.bot_username):
                return False
            return not _is_addressed_to_bot(
                message,
                bot_id=config.bot_id,
                bot_username=config.bot_username,
            )
        fail("unknown group mode: {}".format(group_mode))

    def handle_unauthorized_message(message):
        """Handles messages from chats that are not in `allowed_chats`.

        Unauthorized commands explicitly marked `allow_unauthorized` still run.
        Everything else is either forwarded to the configured owner chat or
        left unhandled, depending on `unauthorized_forward_to`.
        """
        chat_id = tg.chat_id(message)
        if is_allowed_chat(chat_id):
            return False

        set_commands_for_chat([], chat_id)

        command = _parse_command(message, bot_username=config.bot_username)
        if command != None and not command.is_for_this_bot:
            return True

        route = resolve_command_route(command)
        if route != None and route.allow_unauthorized:
            text = unauthorized_command_texts.get(route.name)
            if text != None:
                tg.reply(message, text)
            elif route.require_args and route.args == "":
                tg.reply(message, command_usage_text(route))
            else:
                route.action(message, route.args)
            return True

        if unauthorized_forward_to != None:
            tg.forward_message(unauthorized_forward_to, chat_id, tg.message_id(message))
            if unauthorized_forward_ack != "":
                tg.reply(message, unauthorized_forward_ack)
            print("forwarded message from %s" % chat_id)
            return True

        return False

    def dispatch_command(message):
        """Dispatches a Telegram command if the message starts with one.

        Commands addressed to a different bot are consumed silently. Unknown
        commands addressed to this bot are consumed with `unknown_command_text`.
        Non-command messages return False so fallback handling can run.
        """
        command = _parse_command(message, bot_username=config.bot_username)
        if command != None and not command.is_for_this_bot:
            return True

        route = resolve_command_route(command)
        if route == None:
            if command != None:
                tg.reply(message, unknown_command_text)
                return True
            return False

        if route.require_args and route.args == "":
            tg.reply(message, command_usage_text(route))
            return True

        route.action(message, route.args)
        return True

    def resolve_command_route(command):
        """Combines parsed command data with registered command metadata."""
        if command == None:
            return None

        entry = commands.get(command.name)
        if entry == None:
            return None

        return struct(
            name=entry["name"],
            description=entry["description"],
            action=entry["action"],
            allow_unauthorized=entry["allow_unauthorized"],
            require_args=entry["require_args"],
            usage=entry["usage"],
            args=command.args,
        )

    def command_usage_text(route):
        """Builds the validation message for commands requiring arguments."""
        if route.usage != "":
            return usage_text.format(route.usage)
        return empty_args_text

    def command_names():
        """Returns command names sorted for stable Telegram menu updates."""
        names = []
        for name in commands:
            names.append(name)
        return sorted(names)

    def set_commands(chats=None):
        """Publishes the registered command menu to every supplied chat."""
        if chats == None:
            chats = allowed_chats

        names = command_names()
        for chat_id in chats:
            set_commands_for_chat(names, chat_id)

    def set_commands_for_chat(names, chat_id):
        """Publishes an explicit command-name list for one Telegram chat."""
        payload = []
        for name in names:
            payload.append(
                {
                    "command": name.lstrip("/"),
                    "description": commands[name]["description"],
                }
            )

        tg.call(
            method="setMyCommands",
            args={
                "commands": payload,
                "scope": {
                    "type": "chat",
                    "chat_id": chat_id,
                },
            },
        )

    return module(
        "bot",
        command=command,
        command_names=command_names,
        dispatch_command=dispatch_command,
        handle=handle,
        handle_message=handle_message,
        is_allowed_chat=is_allowed_chat,
        set_commands=set_commands,
        set_commands_for_chat=set_commands_for_chat,
    )


def _command_entities(message):
    """Returns the entity array corresponding to the message text field.

    Telegram keeps plain text entities in `entities` and caption entities in
    `caption_entities`. Command parsing should not need to know which field the
    user sent.
    """
    if "text" in message:
        return message.get("entities", [])
    elif "caption" in message:
        return message.get("caption_entities", [])
    return []


def _parse_command_name(token, bot_username=""):
    """Splits `/command` or `/command@botname` into name and target status."""
    parts = token.split("@", 1)
    name = parts[0]
    if len(parts) == 1:
        return name, True

    target_username = parts[1]
    return name, target_username == bot_username


def _parse_command(message, bot_username=""):
    """Parses the leading Telegram bot command from a message.

    Telegram command semantics are entity-based, not just text-prefix based. A
    slash in ordinary text should not be treated as a command unless Telegram
    marked the first entity at offset zero as `bot_command`.
    """
    entities = _command_entities(message)
    if len(entities) == 0:
        return None

    entity = entities[0]
    if entity["type"] != "bot_command" or entity["offset"] != 0:
        return None

    msg_text = _text(message)
    offset = entity["offset"]
    end = offset + entity["length"]
    token = msg_text[offset:end]
    name, is_for_this_bot = _parse_command_name(token, bot_username=bot_username)

    return struct(
        name=name,
        args=msg_text[end:].strip(),
        is_for_this_bot=is_for_this_bot,
    )


def _is_command(message, bot_username=""):
    command = _parse_command(message, bot_username=bot_username)
    return command != None and command.is_for_this_bot


def _text_field(message):
    """Returns the field that contains user-visible text, if any."""
    if "text" in message:
        return "text"
    elif "caption" in message:
        return "caption"
    return ""


def _text(message):
    """Returns message text or caption, failing on non-text messages."""
    field = _text_field(message)
    if field == "":
        fail("message has no text or caption")
    return message[field]


def _has_text(message):
    return _text_field(message) != ""


def _is_group_chat(message):
    return message["chat"].get("type") in ("group", "supergroup")


def _is_forwarded_message(message):
    return "forward_from" in message


def _is_reply_to_bot(message, bot_id=0):
    """Reports whether the message replies to a message sent by this bot."""
    return (
        "reply_to_message" in message
        and "from" in message["reply_to_message"]
        and message["reply_to_message"]["from"]["id"] == bot_id
    )


def _contains_bot_mention(message, bot_username=""):
    """
    Reports whether the message text contains this bot's username mention.
    """
    if bot_username == "" or not _has_text(message):
        return False
    return "@" + bot_username in _text(message)


def _is_addressed_to_bot(message, bot_id=0, bot_username=""):
    return _is_reply_to_bot(message, bot_id=bot_id) or _contains_bot_mention(
        message,
        bot_username=bot_username,
    )


def _strip_bot_mention(message, bot_username=""):
    """Removes this bot's textual mention from group messages.

    The function mutates the message dictionary, matching the common routing
    use case where downstream command or fallback handlers should see the
    user's prompt without the address marker.
    """
    if bot_username == "" or "chat" not in message or not _is_group_chat(message):
        return message

    field = _text_field(message)
    if field == "":
        return message

    mention = "@" + bot_username
    if mention in message[field]:
        message[field] = message[field].replace(mention, "").strip()
    return message


def _normalize_chat(chat):
    """Returns a compact chat record suitable for prompts, logs, and caches."""
    return {
        "id": chat["id"],
        "type": chat.get("type", ""),
        "title": chat.get("title", ""),
        "username": chat.get("username", ""),
        "display_name": _describe_chat(chat),
    }


def _normalize_user(user):
    """Returns a compact user record suitable for prompts, logs, and caches."""
    return {
        "id": user["id"],
        "first_name": user.get("first_name", ""),
        "last_name": user.get("last_name", ""),
        "username": user.get("username", ""),
        "display_name": _describe_user(user),
    }


def _describe_chat(chat):
    if chat.get("title", "") != "":
        return chat["title"]
    return _describe_user(chat)


def _describe_user(user):
    parts = []
    if user.get("first_name", "") != "":
        parts.append(user["first_name"])
    if user.get("last_name", "") != "":
        parts.append(user["last_name"])
    if len(parts) > 0:
        return " ".join(parts)
    if user.get("username", "") != "":
        return "@" + user["username"]
    return "user {}".format(user["id"])


def _format_username(info):
    if info == None:
        return "no username"
    username = info.get("username", "")
    if username == "":
        return "no username"
    return "@" + username


def _contains_bot_mention_for_config(message):
    return _contains_bot_mention(message, bot_username=config.bot_username)


def _is_addressed_to_bot_for_config(message):
    return _is_addressed_to_bot(
        message,
        bot_id=config.bot_id,
        bot_username=config.bot_username,
    )


def _is_command_for_config(message):
    return _is_command(message, bot_username=config.bot_username)


def _is_reply_to_bot_for_config(message):
    return _is_reply_to_bot(message, bot_id=config.bot_id)


def _parse_command_for_config(message):
    return _parse_command(message, bot_username=config.bot_username)


def _strip_bot_mention_for_config(message):
    return _strip_bot_mention(message, bot_username=config.bot_username)


tgbot = module(
    "tgbot",
    command_entities=_command_entities,
    contains_bot_mention=_contains_bot_mention_for_config,
    describe_chat=_describe_chat,
    describe_user=_describe_user,
    format_username=_format_username,
    has_text=_has_text,
    is_addressed_to_bot=_is_addressed_to_bot_for_config,
    is_command=_is_command_for_config,
    is_forwarded_message=_is_forwarded_message,
    is_group_chat=_is_group_chat,
    is_reply_to_bot=_is_reply_to_bot_for_config,
    new=_new,
    normalize_chat=_normalize_chat,
    normalize_user=_normalize_user,
    parse_command=_parse_command_for_config,
    strip_bot_mention=_strip_bot_mention_for_config,
    text=_text,
    text_field=_text_field,
)
