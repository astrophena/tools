# © 2025 Ilya Mateyko. All rights reserved.
# Use of this source code is governed by the ISC
# license that can be found in the LICENSE.md file.

# vim: ft=starlark shiftwidth=4

"""Provides helper methods for calling the Telegram Bot API."""


def _forward_message(to, from_chat_id, message_id):
    """Forwards a message from one chat to another.

    This function calls the Telegram Bot API's 'forwardMessage' method.

    Args:
        to: The chat ID (integer or string) where the message should be forwarded to.
        from_chat_id: The chat ID (integer or string) from where the message should be forwarded.
        message_id: The ID of the message to forward (integer).

    Returns:
        The result of the 'forwardMessage' Telegram Bot API call.
    """
    return telegram.call(
        method="forwardMessage",
        args={
            "chat_id": to,
            "from_chat_id": from_chat_id,
            "message_id": message_id,
        },
    )


def _send_message(to, text, reply_markup={}, link_preview=False):
    """Sends a text message to a Telegram chat.

    This function handles message splitting for messages exceeding Telegram's
    message length limit (4096 characters). It also applies Markdown formatting
    to the message text and allows disabling link previews.

    Args:
        to: The chat ID (integer or string) where the message should be sent.
        text: The text content of the message to send.
        reply_markup: (optional) A reply markup object (e.g., InlineKeyboardMarkup, ReplyKeyboardMarkup)
            to attach to the message. Defaults to an empty dictionary (no reply markup).
        link_preview: (optional) A boolean indicating whether to disable link previews in the message.
            Defaults to False (link previews are enabled by default in Telegram).

    Returns:
        None. The function sends messages via Telegram Bot API calls.
    """
    args = {
        "chat_id": to,
        "reply_markup": reply_markup,
        "link_preview_options": {
            "is_disabled": True,
        },
    }
    if link_preview:
        args["link_preview_options"]["is_disabled"] = not link_preview

    for chunk in _split_message(text):
        msg = markdown.convert(chunk)
        args |= msg
        telegram.call(
            method="sendMessage",
            args=args,
        )


def _split_message(message):
    """Splits a message into chunks if it exceeds Telegram's message length limit.

    Telegram Bot API has a message length limit (currently 4096 characters).
    This function splits a long message into chunks that are within this limit,
    attempting to split at line breaks to maintain readability.

    Args:
        message: The message string to be split.

    Returns:
        A list of strings, where each string is a chunk of the original message
        that is within the Telegram message length limit. If the original message
        is already within the limit, it returns a list containing only the original message.
    """
    if len(message) <= 4096:
        return [message]

    chunks = []
    current_chunk = ""
    lines = message.split("\n")

    for line in lines:
        if len(current_chunk) + len(line) + 1 <= 4096:
            current_chunk += line + "\n"
        else:
            chunks.append(current_chunk.strip())
            current_chunk = line + "\n"

    chunks.append(current_chunk.strip())
    return chunks


tg = module(
    "tg",
    call=telegram.call,
    forward_message=_forward_message,
    send_message=_send_message,
)
