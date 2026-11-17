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
