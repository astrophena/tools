# © 2025 Ilya Mateyko. All rights reserved.
# Use of this source code is governed by the ISC
# license that can be found in the LICENSE.md file.

# vim: ft=starlark shiftwidth=4

"""Provides helper methods for calling the Telegram Bot API."""


def _forward_message(to, from_chat_id, message_id):
    """Forwards a message and returns the API response."""
    return telegram.call(
        method="forwardMessage",
        args={
            "chat_id": to,
            "from_chat_id": from_chat_id,
            "message_id": message_id,
        },
    )


def _chat_id(message):
    """Returns the chat ID from a Telegram message."""
    return message["chat"]["id"]


def _message_id(message):
    """Returns the message ID from a Telegram message."""
    return message["message_id"]


def _send_message(to, text, reply_markup=None, link_preview=False):
    """Sends Markdown to a chat, splitting long messages. Set link_preview to enable previews."""
    if reply_markup == None:
        reply_markup = {}

    args = {
        "chat_id": to,
        "reply_markup": reply_markup,
        "link_preview_options": {"is_disabled": not link_preview},
    }
    for chunk in _split_message(text):
        telegram.call(method="sendMessage", args=args | markdown.convert(chunk))


def _reply(message, text, reply_markup=None, link_preview=False):
    """Sends a message to the same chat as the received message."""
    _send_message(
        _chat_id(message), text, reply_markup=reply_markup, link_preview=link_preview
    )


def _as_code(value, language=""):
    """Formats a value as a Markdown fenced code block."""
    return "```{}\n{}\n```\n".format(language, value)


def _reply_code(message, value, language=""):
    """Sends a value as a Markdown fenced code block."""
    _reply(message, _as_code(repr(value), language=language))


def _reply_text_as_code(message, text, language=""):
    """Sends text as a Markdown fenced code block."""
    _reply(message, _as_code(text, language=language))


def _send_chat_action(chat_id, action):
    """Sends a Telegram chat action, such as 'typing'."""
    return telegram.call(
        method="sendChatAction",
        args={
            "chat_id": chat_id,
            "action": action,
        },
    )


def _send_typing(chat_id):
    """Sends the 'typing' chat action."""
    return _send_chat_action(chat_id, "typing")


def _set_reaction(chat_id, message_id, emoji):
    """Sets an emoji reaction on a message."""
    return telegram.call(
        method="setMessageReaction",
        args={
            "chat_id": chat_id,
            "message_id": message_id,
            "reaction": [{"type": "emoji", "emoji": emoji}],
        },
    )


def _react(message, emoji):
    """Sets an emoji reaction on the received message."""
    return _set_reaction(_chat_id(message), _message_id(message), emoji)


def _leave_chat(chat_id):
    """Leaves a Telegram chat."""
    return telegram.call(
        method="leaveChat",
        args={
            "chat_id": chat_id,
        },
    )


def _get_largest_photo_file(message):
    """Downloads the largest photo attached to a Telegram message."""
    photos = message.get("photo", [])
    if len(photos) == 0:
        return None

    return telegram.get_file(photos[-1]["file_id"])


def _split_message(message):
    """Splits long messages at line breaks where possible."""
    limit = 4096
    if len(message) <= limit:
        return [message]

    chunks = []
    current_chunk = ""
    lines = message.split("\n")

    for line in lines:
        if len(line) > limit:
            if current_chunk != "":
                chunks.append(current_chunk.strip())
                current_chunk = ""
            for i in range(0, len(line), limit):
                chunks.append(line[i : i + limit])
            continue

        if len(current_chunk) + len(line) + 1 <= limit:
            current_chunk += line + "\n"
        else:
            if current_chunk != "":
                chunks.append(current_chunk.strip())
            current_chunk = line + "\n"

    if current_chunk != "":
        chunks.append(current_chunk.strip())
    return chunks


tg = module(
    "tg",
    as_code=_as_code,
    call=telegram.call,
    chat_id=_chat_id,
    get_largest_photo_file=_get_largest_photo_file,
    leave_chat=_leave_chat,
    message_id=_message_id,
    react=_react,
    forward_message=_forward_message,
    reply=_reply,
    reply_code=_reply_code,
    reply_text_as_code=_reply_text_as_code,
    send_message=_send_message,
    send_chat_action=_send_chat_action,
    send_typing=_send_typing,
    set_reaction=_set_reaction,
    split_message=_split_message,
)
