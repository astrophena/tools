feeds = [
    feed(
        url = "https://example.com/feed.xml",
        block_rule = lambda item: "block" in item.title.lower(),
    )
]
