def send_message(to, text):
    call(
            method = "sendMessage",
            args = {
                "chat_id": to,
                "text": text
            }
    )

update = json.decode(raw_update)

send_message(update["message"]["chat"]["id"], "Hello, world!")
