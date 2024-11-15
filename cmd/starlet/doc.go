// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Starlet is a Telegram bot runner using Starlark.

Starlet acts as an intermediary between the Telegram Bot API and Starlark
code, enabling the creation of Telegram bots using the Starlark scripting
language. It provides a simple way to define bot commands, handle incoming
messages, and interact with the Telegram API.

Starlet periodically pings itself to prevent [Render] from putting it to
sleep, ensuring continuous operation.

# Usage

	$ starlet [flags...]

# Starlark environment

In addition to the standard Starlark modules, the following modules are
available to the bot code:

	config: Contains bot configuration.
		- bot_id (int): Telegram user ID of the bot.
		- bot_username (str): Telegram username of the bot.
		- owner_id (int): Telegram user ID of the bot owner.
		- version (str): Bot version string.

	convcache: Allows to cache bot conversations.
		- get(chat_id: int) -> list: Retrieves the conversation history for the given chat ID.
		- append(chat_id: int, message: str): Appends a new message to the conversation history.
		- reset(chat_id: int): Clears the conversation history for the given chat ID.

	files: Allows to retrieve files from GitHub Gist with bot code.
		- read(name: str) -> str: Retrieves a file from GitHub Gist.

	gemini: Allows interaction with Gemini API.
		- generate_content(model, contents, system, unsafe): Generates text using Gemini:
			- model (str): The name of the model to use for generation.
			- contents (list of strings): The text to be provided to Gemini for generation.
			- system (dict, optional): System instructions to guide Gemini's response, containing a single key "text" with string value.
			- unsafe (bool, optional): Disables all model safety measures.

	markdown: Allows operations with Markdown text.
		- strip(text: str) -> str: Strips out all formatting from Markdown text.

	html: Helper functions for working with HTML.
		- escape(s): Escapes HTML string.

	telegram: Allows sending requests to the Telegram Bot API.
		- call(method, args): Calls a Telegram Bot API method:
			- method (string): The Telegram Bot API method to call.
			- args (dict): The arguments to pass to the method.

	time: Provides time-related functions.

See https://pkg.go.dev/go.starlark.net/lib/time#Module for documentation of
the time module.

# GitHub Gist structure

The GitHub Gist containing the bot code must have the following structure:

  - bot.star: Contains the Starlark code for the bot.
  - error.tmpl (optional): Contains the HTML template for error messages. If omitted,
    a default template will be used. The template receives the error message as %v.

# Entry point

The bot code must define a function called handle that takes a single
argument — a dictionary representing the Telegram update. This function is
called by Starlet for each incoming update.

If you define a function on_load, it will be called by Starlet each time it
loads bot code from GitHub Gist. This can be used, for example, to update
command list in Telegram.

# Environment variables

The following environment variables can be used to configure Starlet:

  - GEMINI_KEY: Gemini API key.
  - GH_TOKEN: GitHub API token.
  - GIST_ID: GitHub Gist ID to load bot code from.
  - HOST: Bot domain used for setting up webhook.
  - TG_OWNER: Telegram user ID of the bot owner.
  - TG_SECRET: Secret token used to validate Telegram Bot API updates.
  - RELOAD_TOKEN: Secret token used to make POST requests to /reload endpoint
    triggering bot code reload from GitHub Gist.
  - TG_TOKEN: Telegram Bot API token.

# Debug interface

Starlet provides a debug interface at /debug with the following endpoints:

  - /debug/code: Displays the currently loaded bot code.
  - /debug/logs: Displays the last 300 lines of logs, streamed automatically.
  - /debug/reload: Reloads the bot code from the GitHub Gist.

Authentication through Telegram is required to access the debug interface
when running on Render. The user must be the bot owner to successfully
authenticate.

See https://core.telegram.org/widgets/login for guidance. Use "https://<bot
URL>/login" as login URL.

[Render]: https://render.com
*/
package main

import (
	_ "embed"

	"go.astrophena.name/tools/internal/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }
