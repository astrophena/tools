# © 2025 Ilya Mateyko. All rights reserved.
# Use of this source code is governed by the ISC
# license that can be found in the LICENSE.md file.

# vim: ft=starlark shiftwidth=4

"""Provides functions to cache conversation history per chat ID.

Attributes:
    get(chat_id: int) -> list: Retrieves the conversation history (list of strings)
        for the given chat ID. Returns an empty list if no history exists.
    append(chat_id: int, message: str) -> None: Appends a message string to the
        conversation history for the given chat ID.
    reset(chat_id: int) -> None: Clears the conversation history for the
        given chat ID.
"""

def _get(chat_id):
    """Retrieves the conversation history for a chat ID."""
    cache_key = _cache_key(chat_id)
    cur = kvcache.get(cache_key)
    if cur == None:
        cur = []
    return cur

def _append(chat_id, message):
    """Appends a message to the conversation history for a chat ID."""
    cur = _get(chat_id)
    cur.append(message)
    kvcache.set(_cache_key(chat_id), cur)

def _reset(chat_id):
    """Resets the conversation history for a chat ID."""
    kvcache.set(_cache_key(chat_id), [])

def _cache_key(chat_id):
    return "convcache_%d" % chat_id

convcache = module(
    "convcache",
    get = _get,
    append = _append,
    reset = _reset,
)
