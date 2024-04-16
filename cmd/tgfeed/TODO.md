# TODO

**I. Edge cases for feed fetching:**

* [ ] Test how tgfeed handles feeds with:
  * [ ] Non-standard XML formats
  * [ ] Empty feeds or no new items
  * [ ] Invalid RSS syntax (errors beyond HTTP status codes)

**II. Content Summarization (if applicable):**

* [ ] Verify successful calls to YandexGPT API for summarization (with token).
* [ ] Test handling errors from the YandexGPT API.
* [ ] Optionally, test the quality of generated summaries using a separate tool.

**III. Filter Logic:**

* [ ] Write tests for complex filter scenarios with regular expressions.
* [ ] Test combining `keep_rule` and `ignore_rule` for a single feed.
* [ ] Verify behavior for empty filter rules or invalid regular expressions.

**IV. Error Handling:**

* [ ] Test handling other potential errors during execution (e.g., parsing JSON, saving state to Gist).
* [ ] Verify appropriate logging of errors.

**V. Command-Line Flags:**

* [ ] Write tests to verify the behavior of flags like:
  * [ ] `-update-feeds`
  * [ ] `-reenable`
  * [ ] `-gc`
