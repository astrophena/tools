# Starlark Environment

These built-in functions and modules are available in the Starlark environment.

## `config`

A module containing configuration information about the bot.

### `config.bot_id`

The Telegram ID of the bot.

### `config.bot_username`

The username of the bot.

### `config.owner_id`

The Telegram ID of the bot owner.

### `config.version`

The version of the bot.

## `debug`

A module containing debugging utilities.

### `debug.stack()`

Returns a string describing the current call stack.

### `debug.go_stack()`

Returns a string describing the Go call stack.

## `fail()`

Terminates execution with a specified error message.

## `files`

A module for accessing files provided to the bot.

### `files.read()`

Reads the content of a file.

## `gemini`

A module for interacting with the Google Gemini API.

## `kvcache`

A module for caching key-value pairs.

## `markdown`

A module for Markdown conversion.

### `markdown.convert()`

Converts a Markdown string to a Telegram message struct.

## `module()`

Creates a new Starlark module from a dictionary.

## `struct()`

Creates a new Starlark struct from a dictionary.

## `telegram`

A module for interacting with the Telegram Bot API.

## `time`

A module for time-related functions.
