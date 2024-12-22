def filter_description(item):
    if not "media" in item.extensions:
        return False
    return "Some" in item.extensions["media"]["group"][0]["children"]["description"][0]["value"]

feeds = [
    feed(
        url = "https://example.com/feed.xml",
        keep_rule = filter_description,
    )
]
