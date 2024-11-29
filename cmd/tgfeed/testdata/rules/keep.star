feeds = [
    feed(
        url = "https://example.com/feed.xml",
        keep_rule = lambda item: "keep" in item.title.lower(),
    )
]
