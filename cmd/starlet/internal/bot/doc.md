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

## `fail(err: str)`

Terminates execution with a specified error message.

## `files`

A module for accessing files provided to the bot.

### `files.read(name: str)`

Reads the content of a file.

## `llm`

LLM provider.

The module sends chat-style contents, optional image bytes, and optional
instructions to the configured OpenAI Responses-compatible /responses endpoint.
It returns the response's text output as a single string and records token usage
under a caller-provided usage key.

This is useful with the OpenAI API and with other AI providers that expose a
compatible endpoint, such as OpenRouter.

This module provides two functions: generate and usage.

usage(key, date?) returns cumulative token usage for the key.
If date is provided (YYYY-MM-DD), it returns usage only for that exact UTC day.

generate accepts the following keyword arguments:

  - model (str): Model name to generate with.
  - contents (list of (str, str) tuples): A list of (role, content) messages.
  - usage_key (str): Arbitrary key used to accumulate persistent token usage stats.
  - image (bytes, optional): Optional raw image bytes to upload as input_image.
  - instructions (str, optional): Optional high-level instructions for the model.

For example:

	text = llm.generate(
	    model="gpt-4.1-mini",
	    contents=[
	        ("user", "Describe this image briefly.")
	    ],
	    image=files.read("cat.jpg"),
	    usage_key="chat:123",
	    instructions="Be concise."
	)

The return value is a single string from the response output message text.

## `kvcache`

This module provides two functions for using a simple key-value cache:

  - get(key: str) -> any | None: Retrieves the value associated with the
    given string key. Returns the stored value if the key exists and has
    not expired. Returns None if the key is not found or if the entry
    has expired. Accessing a key resets its TTL timer.
  - set(key: str, value: any): Stores the given value under the specified
    string key. Any existing value for the key is overwritten. Storing a
    value resets the TTL timer for that key.

## `markdown`

A module for Markdown conversion.

### `markdown.convert(s: str)`

Converts a Markdown string to a Telegram message struct.

## `module(name: str, **members)`

Instantiates a module struct with the name from the specified keyword arguments.

## `struct(**fields)`

Instantiates an immutable struct from the specified keyword arguments.

## `telegram`

This module provides two functions for working with the Telegram Bot API: call and get_file.

The call function takes two arguments:

  - method (str): The Telegram Bot API method to call.
  - args (dict): The arguments to pass to the method.

For example, to send a message to a chat:

	response = telegram.call(
	    method="sendMessage",
	    args={
	        "chat_id": 123456789,
	        "text": "Hello, world!",
	    }
	)

The response variable will contain the response from the Telegram Bot API.

The get_file function takes one argument:

  - file_id (str): The ID of the file to download.

It returns the content of the file as bytes. For example:

	file_content = telegram.get_file(file_id="...")

## `time`

A module for time-related functions. See https://pkg.go.dev/go.starlark.net/lib/time#Module.
