// Code generated by gendocs.go; DO NOT EDIT.

package main

import "go.astrophena.name/tools/internal/cli"

const helpDoc = `
Starlet allows to create and manage a Telegram bot using the Starlark scripting
language.

Starlet serves an HTTP server to handle Telegram webhook updates and bot
maintenance. The bot's code is sourced from a specified GitHub Gist.
Also Starlet includes features such as user authentication via the Telegram
login widget, ensuring that only the designated bot owner can manage the bot.
In production environments, Starlet periodically pings itself to prevent the
hosting service from putting it to sleep, ensuring continuous operation.

# Starlark language

See Starlark spec for reference.

Additional modules and functions available from bot code:

  - call: Make HTTP POST requests to the Telegram API, facilitating bot commands
    and interactions.
  - escape_html: Escape HTML string.
  - json: The Starlark JSON module, enabling JSON parsing and encoding.
  - time: The Starlark time module, providing time-related functions.

[Starlark spec]: https://github.com/bazelbuild/starlark/blob/master/spec.md
`

func init() { cli.SetDescription(helpDoc) }
