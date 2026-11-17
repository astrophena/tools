`kvcache.get(key)` returns the cached value or `None` if the key is missing or
expired. A successful read resets the expiration timer.

`kvcache.set(key, value)` stores or replaces a value and resets its expiration
timer. Keys are strings. Values can be `None`, booleans, strings, signed 64-bit
integers, finite floats, lists, tuples, structs, or dictionaries with string keys.
