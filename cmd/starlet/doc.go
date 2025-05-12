// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Starlet is a Telegram bot runner powered by Starlark.

It fetches bot logic written in Starlark from a specified GitHub Gist,
executes it within a Starlark interpreter, and handles incoming Telegram updates
via webhooks. Starlet provides a sandboxed environment with pre-defined modules
for interacting with Telegram, Gemini AI, key-value caching, and more.

When a Telegram update arrives, Starlet parses it, converts the JSON payload
into a Starlark dictionary, and invokes the handle function defined in the
user's bot.star script. Errors during execution are caught and reported back
to the bot owner via Telegram, using a customizable error template.

Starlet is designed for deployment on platforms like Render and includes features
like self-pinging to prevent idling on free tiers.

# Usage

	$ starlet [flags...]

# Starlark Environment

The following built-in functions and modules are available within the bot.star script:

	config: A struct containing bot configuration information.
		- bot_id (int): The Telegram user ID of the bot itself.
		- bot_username (str): The Telegram username of the bot itself.
		- owner_id (int): The Telegram user ID of the designated bot owner.
		- version (str): The version string of the running Starlet instance.

	debug: A module providing debugging utilities.
		- stack() -> str: Returns the current Starlark call stack as a formatted string.
		- go_stack() -> str: Returns the current Go runtime call stack as a string.

	fail(err: str): Causes the current Starlark execution to fail with the provided error message.
	    This error will be reported back to the bot owner.

	files: A module for accessing other files within the same GitHub Gist.
		- read(name: str) -> str: Reads the content of the file named name from the Gist.
		  Raises an error if the file does not exist in the Gist.

	gemini: A module for interacting with Google's Gemini API (requires GEMINI_KEY env var).
		- generate_content(model: str, contents: list[str], system_instructions: str | None = None, unsafe: bool = False) -> list[list[str]]:
		  Generates content using the specified Gemini model.
		  - model: Name of the Gemini model (e.g., "gemini-1.5-flash").
		  - contents: A list of strings representing the conversation history or prompt parts.
		    Odd-indexed elements are treated as user input, even-indexed as model output (if len > 1).
		  - system_instructions: Optional system prompt to guide the model's behavior.
		  - unsafe: If True, disables safety filters (use with caution).
		  Returns a list of candidate responses, where each candidate is a list of text parts (strings).

	kvcache: A module providing a simple in-memory key-value cache with time-to-live (TTL).
		- get(key: str) -> value | None: Retrieves the value for key. Returns None if the key
		  doesn't exist or has expired. Accessing a key resets its TTL.
		- set(key: str, value: any): Stores value under key, overwriting any existing entry.
		  Setting a key resets its TTL. TTL is based on last access/modification time.

	markdown: A module for Markdown processing.
		- convert(s: str) -> dict: Converts the input Markdown string s into a dictionary
		  representing a Telegram message with entities, suitable for use with the
		  telegram.call method (e.g., as part of the args for sendMessage).

	module(name: str, **members): Creates a new Starlark module object.

	struct(**fields): Creates a new Starlark struct object.

	telegram: A module for interacting with the Telegram Bot API.
		- call(method: str, args: dict) -> any: Makes a call to the specified Telegram Bot API
		  method with the given args dictionary. Returns the decoded JSON response
		  from the Telegram API as a Starlark value (usually a dict or list).

	time: The standard Starlark time module. Provides functions for time manipulation,
	      formatting, and parsing. See https://pkg.go.dev/go.starlark.net/lib/time#Module
	      for detailed documentation.

# Starlet Standard Library

Additionally, Starlet provides a small standard library that can be loaded from within bot.star using the @starlet// prefix:

	load("@starlet//convcache.star", "convcache")
	load("@starlet//tg.star", "tg")

Modules available:

	convcache: Provides functions (get, append, reset) to manage simple conversation
	           histories per chat ID using the kvcache.

	tg: Provides helper functions built on top of telegram.call:
	    - forward_message(to: int|str, from_chat_id: int|str, message_id: int): Forwards a message.
	    - send_message(to: int|str, text: str, reply_markup: dict = {}, link_preview: bool = False):
	      Sends a message, automatically handling Markdown conversion via markdown.convert
	      and splitting long messages (>4096 chars) into multiple chunks.

# GitHub Gist Structure

The bot's code and configuration reside in a GitHub Gist, which must contain:

  - bot.star: The main Starlark script containing the bot's logic. Must define the handle function.
  - error.tmpl (optional): A Go template string used for formatting error messages sent to the owner.
    If omitted, a default template is used. The template receives the error string via %v.

Any other files (e.g., .json, .txt) can be included and accessed from bot.star using:

	files.read("filename.ext")

# Entry Point

The bot.star script *must* define a function named handle that accepts a single argument:

	def handle(update):
	    # update is a Starlark dictionary representing the received Telegram update JSON.
	    # Bot logic goes here.
	    pass

Optionally, you can define an on_load function:

	def on_load():
	    # Code here runs every time the bot code is loaded from the Gist,
	    # including the initial startup and after a reload via the debug interface.
	    # Useful for setup tasks like setting bot commands.
	    pass

Starlet calls handle for each incoming Telegram update webhook. It calls on_load after successfully loading or reloading the code from the Gist.

# Environment Variables

Starlet is configured using environment variables:

Required:

  - GIST_ID: The ID of the GitHub Gist containing the bot.star file.
  - TG_OWNER: The numeric Telegram User ID of the bot's owner (receives error reports, can access debug interface).
  - TG_TOKEN: The token for your Telegram Bot API.

Optional:

  - GEMINI_KEY: API key for Google Gemini (required to use the gemini module).
  - GH_TOKEN: A GitHub Personal Access Token (PAT) with gist scope. Recommended for higher rate limits.
  - HOST: The publicly accessible domain name for the bot (e.g., mybot.example.com). Used for setting the Telegram webhook. Required in production (-prod flag).
  - RELOAD_TOKEN: A secret token. If set, enables reloading the bot code from the Gist by sending a POST request to /reload with header "Authorization: Bearer <token>".
  - TG_SECRET: A secret token passed to Telegram when setting the webhook (X-Telegram-Bot-Api-Secret-Token). Telegram includes this token in the header of webhook requests, and Starlet verifies it.

# Debug Interface

When not in production mode, or when accessed by the authenticated bot owner in production mode, Starlet provides a debug interface at /debug:

  - /debug/: Shows basic bot info, loaded Starlark modules, and links to other debug pages.
  - /debug/code: Displays the content of all files currently loaded from the Gist. (Requires auth in prod)
  - /debug/logs: Streams the last 300 lines of logs in real-time. (Requires auth in prod)
  - /debug/reload: A button/link to trigger an immediate reload of the bot code from the GitHub Gist. (Requires auth in prod)

Authentication for the debug interface in production mode uses Telegram Login Widget. The bot owner must authenticate via Telegram. The login callback URL should be set to https://<your-bot-host>/login in BotFather (/setdomain).

[Starlark]: https://starlark.dev/
[Render]: https://render.com/
*/
package main

import (
	_ "embed"

	"go.astrophena.name/base/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }
