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

# Usage

	$ starlet

# Starlark Environment

See https://bot.astrophena.name/env.

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
  - HOST: The publicly accessible domain name for the bot (e.g., mybot.example.com). Used for setting the Telegram webhook.
  - TG_OWNER: The numeric Telegram User ID of the bot's owner (receives error reports).
  - TG_SECRET: A secret token passed to Telegram when setting the webhook (X-Telegram-Bot-Api-Secret-Token). Telegram includes this token in the header of webhook requests, and Starlet verifies it.
  - TG_TOKEN: The token for your Telegram Bot API.

Optional:

  - ADDR: Address for the public server (webhooks, docs), in form of host:port, defaults to localhost:3000.
  - ADMIN_ADDR: Address for the admin server (debug interface). If not set, the admin server is disabled. Can be a Unix socket path (e.g., unix://run/starlet/admin-socket).
  - DATABASE_PATH: Path to a SQLite database file. If not provided, an in-memory store is used.
  - GEMINI_KEY: API key for Google Gemini (required to use the gemini module).
  - GH_TOKEN: A GitHub Personal Access Token (PAT) with gist scope. Recommended for higher rate limits.

# Admin Interface

Starlet provides a debug and admin interface, served on the address specified by ADMIN_ADDR. Access control is expected to be handled at the network level (e.g., a Unix socket with restricted permissions or a firewall).

The interface includes:

  - /debug/: Shows basic bot info, loaded Starlark modules, and links to other debug pages.
  - /debug/logs: Streams the last 300 lines of logs in real-time.
  - /debug/reload: Triggers an immediate reload of the bot code from the GitHub Gist.

[Starlark]: https://starlark-lang.org/
*/
package main

import (
	_ "embed"

	"go.astrophena.name/base/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }
