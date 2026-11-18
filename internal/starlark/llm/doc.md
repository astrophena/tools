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
