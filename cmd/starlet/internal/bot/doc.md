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

## `gemini`

This module provides a single function, generate_content, which uses the
Gemini API to generate text, optionally with an image as context.

It accepts the following keyword arguments:

  - model (str): The name of the model to use for generation (e.g., "gemini-1.5-flash").
  - contents (list of (str, str) tuples): A list of (role, text) tuples representing
    the conversation history. Valid roles are typically "user" and "model".
  - image (bytes, optional): The raw bytes of an image to include. The image is
    inserted as a new part just before the last part of the 'contents'.
    This is useful for multimodal prompts (e.g., asking a question about an image).
  - system_instructions (str, optional): System instructions to guide Gemini's response.
  - unsafe (bool, optional): If set to true, disables all safety settings for the
    content generation, allowing potentially harmful content. Use with caution.

For example, for a text-only prompt:

	responses = gemini.generate_content(
	    model="gemini-1.5-flash",
	    contents=[
	        ("user", "Once upon a time,"),
	        ("model", "there was a brave knight."),
	        ("user", "What happened next?")
	    ],
	    system_instructions="You are a creative story writer. Write a short story based on the provided prompt."
	)

To ask a question about an image:

	image_data = ... # read image file content as bytes
	responses = gemini.generate_content(
	    model="gemini-1.5-flash",
	    contents=[
	        ("user", "Describe this image in detail.")
	    ],
	    image=image_data
	)

The responses variable will contain a list of generated responses, where each response
is a list of strings representing the parts of the generated content.

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
