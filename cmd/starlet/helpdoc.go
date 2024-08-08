// Code generated by devtools/genhelpdoc.go; DO NOT EDIT.

package main

const helpDoc = `
Starlet is a Telegram bot runner using Starlark.

Starlet acts as an intermediary between the Telegram Bot API and Starlark code,
enabling the creation of Telegram bots using the Starlark scripting language.
It provides a simple way to define bot commands, handle incoming messages,
and interact with the Telegram API.

Starlet periodically pings itself to prevent [Render] from putting it to sleep,
ensuring continuous operation.

# Starlark environment

In addition to the standard Starlark modules, the following modules are
available to the bot code:

    config: Contains bot configuration.
    	- owner_id (int): Telegram user ID of the bot owner.
    	- version (str): Bot version string.

    convcache: Allows to cache bot conversations.
    	- get(chat_id: int) -> list: Retrieves the conversation history for the given chat ID.
    	- append(chat_id: int, message: str): Appends a new message to the conversation history.
    	- reset(chat_id: int): Clears the conversation history for the given chat ID.

    gemini: Allows interaction with Gemini API.
    	- generate_content(contents, *, system=None): Generates text using Gemini:
    	  - contents (list of strings): The text to be provided to Gemini for generation.
    	  - system (dict, optional): System instructions to guide Gemini's response, containing a single key "text" with string value.
    	  - chat_mode (bool, False by default): Whether to mark each even string in contents as generated by model.
    		- dump_request (bool, False by default): Whether to log request body for inspection.

    html: Helper functions for working with HTML.
    	- escape(s): Escapes HTML string.

    telegram: Allows sending requests to the Telegram Bot API.
    	- call(method, args): Calls a Telegram Bot API method:
    	  - method (string): The Telegram Bot API method to call.
    	  - args (dict): The arguments to pass to the method.

    time: Provides time-related functions.

See https://pkg.go.dev/go.starlark.net/lib/time#Module for documentation of the
time module.

# GitHub Gist structure

The GitHub Gist containing the bot code must have the following structure:

  - bot.star: Contains the Starlark code for the bot.
  - error.tmpl (optional): Contains the HTML template for error messages.
    If omitted, a default template will be used. The template receives the error
    message as %v.

# Entry point

The bot code must define a function called handle that takes a single argument
— a dictionary representing the Telegram update. This function is called by
Starlet for each incoming update.

# Environment variables

The following environment variables can be used to configure Starlet:

  - TG_TOKEN: Telegram Bot API token.
  - TG_SECRET: Secret token used to validate Telegram Bot API updates.
  - TG_OWNER: Telegram user ID of the bot owner.
  - GH_TOKEN: GitHub API token.
  - GEMINI_KEY: Gemini API key.
  - GIST_ID: GitHub Gist ID to load bot code from.

# Debug interface

Starlet provides a debug interface at /debug with the following endpoints:

  - /debug/code: Displays the currently loaded bot code.
  - /debug/logs: Displays the last 300 lines of logs, streamed automatically.
  - /debug/reload: Reloads the bot code from the GitHub Gist.

Authentication through Telegram is required to access the debug interface when
running on Render. The user must be the bot owner to successfully authenticate.

See https:core.telegram.org/widgets/login for guidance. Use "https:<bot
URL>/login" as login URL.

[Render]: https:render.com
`
