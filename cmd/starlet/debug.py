#!/usr/bin/env python

import json

# Create a shim for json.decode to use json.loads.
json.decode = json.loads

# Read the Starlark bot script from the bot.star file.
with open("bot.star", "r") as f:
    bot_script = f.read()

# Mock data to simulate a Telegram update.
raw_update = json.dumps({
    "message": {
        "chat": {"id": 12345},
        "message_id": 54321,
    }
})

# Mock call function to simulate API calls made by the bot script.
def call(method, args):
    print(f"Calling method: {method} with arguments: {args}.")

# Execute the Starlark bot script in the context of the current global
# namespace.
exec(bot_script, globals())
