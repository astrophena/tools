// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Code generated by devtools/gen/info.go; DO NOT EDIT.

package main

import "go.astrophena.name/tools/internal/cli"

func init() {
	cli.SetInfo(cli.Info{Name: ".", Description: "Starlet is a Telegram bot runner using Starlark.\n\nStarlet acts as an intermediary between the Telegram Bot API and Starlark code,\nenabling the creation of Telegram bots using the Starlark scripting language.\nIt provides a simple way to define bot commands, handle incoming messages,\nand interact with the Telegram API.\n\nStarlet periodically pings itself to prevent Render from putting it to sleep,\nensuring continuous operation.\n\n# Usage\n\n    $ starlet [flags...]\n\n# Starlark environment\n\nIn addition to the standard Starlark modules, the following modules are\navailable to the bot code:\n\n    config: Contains bot configuration.\n    \t- bot_id (int): Telegram user ID of the bot.\n    \t- bot_username (str): Telegram username of the bot.\n    \t- owner_id (int): Telegram user ID of the bot owner.\n    \t- version (str): Bot version string.\n\n    convcache: Allows to cache bot conversations.\n    \t- get(chat_id: int) -> list: Retrieves the conversation history for the given chat ID.\n    \t- append(chat_id: int, message: str): Appends a new message to the conversation history.\n    \t- reset(chat_id: int): Clears the conversation history for the given chat ID.\n\n    files: Allows to retrieve files from GitHub Gist with bot code.\n    \t- read(name: str) -> str: Retrieves a file from GitHub Gist.\n\n    gemini: Allows interaction with Gemini API.\n    \t- generate_content(model, contents, system, unsafe): Generates text using Gemini:\n    \t\t- model (str): The name of the model to use for generation.\n    \t\t- contents (list of strings): The text to be provided to Gemini for generation.\n    \t\t- system (dict, optional): System instructions to guide Gemini's response, containing a single key \"text\" with string value.\n    \t\t- unsafe (bool, optional): Disables all model safety measures.\n\n    markdown: Allows operations with Markdown text.\n    \t- strip(text: str) -> str: Strips out all formatting from Markdown text.\n\n    html: Helper functions for working with HTML.\n    \t- escape(s): Escapes HTML string.\n\n    telegram: Allows sending requests to the Telegram Bot API.\n    \t- call(method, args): Calls a Telegram Bot API method:\n    \t\t- method (string): The Telegram Bot API method to call.\n    \t\t- args (dict): The arguments to pass to the method.\n\n    time: Provides time-related functions.\n\nSee https://pkg.go.dev/go.starlark.net/lib/time#Module for documentation of the\ntime module.\n\n# GitHub Gist structure\n\nThe GitHub Gist containing the bot code must have the following structure:\n\n  - bot.star: Contains the Starlark code for the bot.\n  - error.tmpl (optional): Contains the HTML template for error messages.\n    If omitted, a default template will be used. The template receives the error\n    message as %v.\n\n# Entry point\n\nThe bot code must define a function called handle that takes a single argument\n— a dictionary representing the Telegram update. This function is called by\nStarlet for each incoming update.\n\nIf you define a function on_load, it will be called by Starlet each time it\nloads bot code from GitHub Gist. This can be used, for example, to update\ncommand list in Telegram.\n\n# Environment variables\n\nThe following environment variables can be used to configure Starlet:\n\n  - GEMINI_KEY: Gemini API key.\n  - GH_TOKEN: GitHub API token.\n  - GIST_ID: GitHub Gist ID to load bot code from.\n  - HOST: Bot domain used for setting up webhook.\n  - TG_OWNER: Telegram user ID of the bot owner.\n  - TG_SECRET: Secret token used to validate Telegram Bot API updates.\n  - RELOAD_TOKEN: Secret token used to make POST requests to /reload endpoint\n    triggering bot code reload from GitHub Gist.\n  - TG_TOKEN: Telegram Bot API token.\n\n# Debug interface\n\nStarlet provides a debug interface at /debug with the following endpoints:\n\n  - /debug/code: Displays the currently loaded bot code.\n  - /debug/logs: Displays the last 300 lines of logs, streamed automatically.\n  - /debug/reload: Reloads the bot code from the GitHub Gist.\n\nAuthentication through Telegram is required to access the debug interface when\nrunning on Render. The user must be the bot owner to successfully authenticate.\n\nSee https://core.telegram.org/widgets/login for guidance. Use \"https://<bot\nURL>/login\" as login URL.\n\n[Render]: https://render.com\n"})
}
