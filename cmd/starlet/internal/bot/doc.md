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

`llm.generate(model, contents, usage_key, image?, instructions?)`
returns text from the configured Responses API provider. Pass a list of
`(role, text)` tuples as `contents`. Pass raw bytes as `image` and a string as `instructions`.
Token counts are recorded under `usage_key` when usage tracking is enabled.

```python
text = llm.generate(
    model="gpt-4.1-mini",
    contents=[("user", "Describe this image.")],
    usage_key="chat:123",
    image=files.read("cat.jpg"),
    instructions="Be concise.",
)
```

`llm.usage(key, date="")` returns `input_tokens`, `output_tokens`, and
`total_tokens`. Pass a date in `YYYY-MM-DD` format for a single UTC day.

## `kvcache`

`kvcache.get(key)` returns the cached value or `None` if the key is missing or
expired. A successful read resets the expiration timer.

`kvcache.set(key, value)` stores or replaces a value and resets its expiration
timer. Keys are strings. Values can be `None`, booleans, strings, signed 64-bit
integers, finite floats, lists, tuples, structs, or dictionaries with string keys.

## `markdown`

A module for Markdown conversion.

### `markdown.convert(s: str)`

Converts a Markdown string to a Telegram message struct.

## `module(name: str, **members)`

Instantiates a module struct with the name from the specified keyword arguments.

## `struct(**fields)`

Instantiates an immutable struct from the specified keyword arguments.

## `telegram`

`telegram.call(method=..., args=...)` calls a Bot API method with a dictionary
of arguments and returns the decoded response. Both arguments must be passed by
keyword.

```python
response = telegram.call(
    method="sendMessage",
    args={"chat_id": 123456789, "text": "Hello, world!"},
)
```

`telegram.get_file(file_id)` downloads a Telegram file and returns its bytes.

## `time`

A module for time-related functions. See https://pkg.go.dev/go.starlark.net/lib/time#Module.
