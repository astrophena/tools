# © 2025 Ilya Mateyko. All rights reserved.
# Use of this source code is governed by the ISC
# license that can be found in the LICENSE.md file.

# vim: ft=starlark shiftwidth=4

"""Stores (role, text) messages by chat ID. Missing histories read as empty lists."""


def _get(chat_id):
    """Retrieves the conversation history for a chat ID."""
    history = kvcache.get(_cache_key(chat_id))
    return [] if history == None else history


def _append(chat_id, role, text):
    """Appends a message (role, text) to the conversation history for a chat ID."""
    history = _get(chat_id)
    history.append((role, text))
    kvcache.set(_cache_key(chat_id), history)


def _reset(chat_id):
    """Resets the conversation history for a chat ID."""
    kvcache.set(_cache_key(chat_id), [])


def _cache_key(chat_id):
    return "convcache_%d" % chat_id


convcache = module(
    "convcache",
    get=_get,
    append=_append,
    reset=_reset,
)
