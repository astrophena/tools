bot_owner_id = 853674576

def forward_message(to, from_chat_id, message_id):
    call(
        method = "forwardMessage",
        args = {
            "chat_id": to,
            "from_chat_id": from_chat_id,
            "message_id": message_id,
        },
    )

def send_message(to, text):
    call(
            method = "sendMessage",
            args = {
                "chat_id": to,
                "text": text
            }
    )

def process_message(message):
    from_chat_id = message["chat"]["id"]
    if from_chat_id == bot_owner_id:
        send_message(bot_owner_id, "👋 Привет!")
        return
    forward_message(bot_owner_id, from_chat_id, message["message_id"])

def process_update(raw_update):
    update = json.decode(raw_update)

    if "message" in update:
        process_message(update["message"]
