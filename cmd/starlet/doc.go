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

	$ starlet [directory]

If a directory is provided, Starlet runs in development mode. Otherwise, it
runs in production mode.

# Development Mode

When running in development mode, Starlet serves a bot debugger interface at
http://localhost:3000 (or the address specified by the ADDR environment
variable). This interface allows you to interact with your bot in a chat-like
UI and inspect the Telegram API calls it makes.

In this mode, instead of fetching code from a GitHub Gist, Starlet loads it
from the provided directory. It also watches for file changes in the directory
and automatically reloads the bot when a file is modified, created, or deleted.

The Telegram Bot API calls are intercepted, and responses are mocked. This
allows for local development and testing without needing to expose your bot to
the internet or use a real Telegram bot token.

The directory should have the same structure as the GitHub Gist.

# Starlark Environment

See https://github.com/astrophena/tools/blob/master/cmd/starlet/internal/bot/doc.md.

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

  - ADDR: Address to listen for HTTP requests, in form of host:port, defaults to localhost:3000.
  - GEMINI_KEY: API key for Google Gemini (required to use the gemini module).
  - GEMINI_PROXY_TOKEN: A secret token for authenticating with the Gemini proxy.
  - GH_TOKEN: A GitHub Personal Access Token (PAT) with gist scope. Recommended for higher rate limits.
  - HOST: The publicly accessible domain name for the bot (e.g., mybot.example.com). Used for setting the Telegram webhook. Required in production.
  - RELOAD_TOKEN: A secret token. If set, enables reloading the bot code from the Gist by sending a POST request to /reload with header "Authorization: Bearer <token>".
  - TG_SECRET: A secret token passed to Telegram when setting the webhook (X-Telegram-Bot-Api-Secret-Token). Telegram includes this token in the header of webhook requests, and Starlet verifies it.

# Debug Interface

When not in production mode, or when accessed by the authenticated bot owner in production mode, Starlet provides a debug interface at /debug:

  - /debug/: Shows basic bot info, loaded Starlark modules, and links to other debug pages.
  - /debug/bot: A bot debugger with a chat interface and a log of intercepted Telegram API calls. Available only in development mode.
  - /debug/logs: Streams the last 300 lines of logs in real-time. (Requires auth in prod)
  - /debug/reload: A button/link to trigger an immediate reload of the bot code from the GitHub Gist. (Requires auth in prod)

Authentication for the debug interface in production mode uses Telegram Login Widget. The bot owner must authenticate via Telegram. The login callback URL should be set to https://<your-bot-host>/login in BotFather (/setdomain).

# Gemini Proxy

Starlet includes an optional, built-in Gemini API proxy at /gemini. This
proxy is intended for the author's other pet projects and is not essential for
the bot's core functionality. It allows other applications to access the Gemini
API without exposing the actual API key. Access to the proxy is protected by a
bearer token (see the GEMINI_PROXY_TOKEN environment variable).

[Starlark]: https://starlark-lang.org/
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
