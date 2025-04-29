// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Starlet is a Telegram bot runner that uses Starlark.

Starlet fetches your bot code from a GitHub Gist, executes it within a [Starlark]
interpreter, and exposes specific modules for interacting with Telegram and
other services. When a Telegram update arrives via webhook, Starlet parses it,
converts it to a Starlark dictionary, and calls the handle function defined in
your bot's Starlark code.

Starlet periodically pings itself to prevent Render from idling, ensuring
continuous operation.

# Usage

	$ starlet [flags...]

# Starlark Environment

The following built-in functions and modules are available to your bot code:

	config: Contains bot configuration.
		- bot_id (int): The Telegram user ID of the bot.
		- bot_username (str): The Telegram username of the bot.
		- owner_id (int): The Telegram user ID of the bot owner.
		- version (str): The bot's version string.

	kvcache: Provides a simple key-value cache.
		- get(key: str) -> value | None: Retrieves the value for the key. Returns None if not found or expired. Resets TTL on access.
		- set(key: str, value: any) -> None: Stores the value under the key. Resets TTL on set.

	debug: Provides debugging utilities.
		- stack(): Returns the current Starlark call stack as a string.
		- go_stack(): Returns the current Go call stack as a string.

	eval(code: str, environ: dict, optional) -> str: Executes Starlark code with the given environment and returns the all it's output.

	files: Allows retrieval of files from the GitHub Gist containing the bot code.
		- read(name: str) -> str: Retrieves a file from the GitHub Gist. Causes an error if the file is not present.

	gemini: Enables interaction with the Gemini API.
		- generate_content(model, contents, system, unsafe): Generates text using Gemini.
			- model (str): The name of the model to use for generation.
			- contents (list of strings): The text to provide to Gemini for generation.
			- system (dict, optional): System instructions to guide Gemini's response, containing a single key "text" with a string value.
			- unsafe (bool, optional): Disables all model safety measures.

	markdown: Facilitates operations with Markdown text.
		- convert(text: str) -> dict: Converts Markdown text to Telegram-flavored message markup suitable for inclusion in sendMessage Telegram Bot API call payloads.

	telegram: Allows interaction with the Telegram Bot API.
		- call(method, args): Calls a Telegram Bot API method.
			- method (str): The Telegram Bot API method to call.
			- args (dict): The arguments to pass to the method.

	time: Provides time-related functions. See https://pkg.go.dev/go.starlark.net/lib/time#Module for documentation.

# GitHub Gist Structure

The GitHub Gist containing the bot code must have the following structure:

  - bot.star: Contains the Starlark code for the bot.
  - error.tmpl (optional): Contains the Markdown template for error messages. If omitted, a default template is used. The template receives the error message as %v.

# Entry Point

The bot code must define a function called handle that takes a single argument — a dictionary representing the Telegram update. This function is called by Starlet for each incoming update.

If you define an on_load function, Starlet will call it whenever it loads bot code from the GitHub Gist, including the initial load and after reloads triggered via the /reload endpoint. This can be useful for tasks like updating the command list in Telegram.

# Environment Variables

The following environment variables can be used to configure Starlet:

  - GEMINI_KEY: The Gemini API key.
  - GH_TOKEN: The GitHub API token.
  - GIST_ID: The GitHub Gist ID from which to load the bot code.
  - HOST: The bot domain used for setting up the webhook.
  - RENDER: Set to "true" if running on Render.
  - RELOAD_TOKEN: A secret token for making POST requests to the /reload endpoint, which triggers a reload of the bot code from the GitHub Gist.
  - TG_OWNER: The Telegram user ID of the bot owner.
  - TG_SECRET: The secret token used to validate Telegram Bot API updates.
  - TG_TOKEN: The Telegram Bot API token.

# Debug Interface

Starlet provides a debug interface at /debug with the following endpoints:

  - /debug/code: Displays the currently loaded bot code.
  - /debug/logs: Displays the last 300 lines of logs, streamed automatically.
  - /debug/reload: Reloads the bot code from the GitHub Gist.

When running on Render, authentication through Telegram is required to access the debug interface. The user must be the bot owner to authenticate successfully. See https://core.telegram.org/widgets/login for guidance. Use https://<bot URL>/login as the login URL.

[Starlark]: https://starlark-lang.org

[Render]: https://render.com
*/
package main

import (
	_ "embed"

	"go.astrophena.name/base/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }
