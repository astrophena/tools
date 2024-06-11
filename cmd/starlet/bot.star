bot_owner_id = 853674576

_commands = {}

def forward_message(to, from_chat_id, message_id):
    call(
        method = "forwardMessage",
        args = {
            "chat_id": to,
            "from_chat_id": from_chat_id,
            "message_id": message_id,
        },
    )

def send_message(to, text, reply_markup = {}):
    call(
        method = "sendMessage",
        args = {
            "chat_id": to,
            "text": text,
            "reply_markup": reply_markup
        }
    )

def is_command(message):
    """
    Reports if the message contains a command.
    """
    if "entities" in message and len(message["entities"]) == 1:
        return message["entities"][0]["type"] == "bot_command"
    return False

def get_command(message):
    """
    Extracts a command from message.
    """
    entity = message["entities"][0]
    name = message["text"][entity["offset"]:entity["length"]]
    return _commands.get(name)

def register_command(command, f):
    _commands[command] = f

def process_message(message):
    from_chat_id = message["chat"]["id"]
    # Forward messages not from the bot owner to it.
    if from_chat_id != bot_owner_id:
        forward_message(bot_owner_id, from_chat_id, message["message_id"])
        return
    # Handle commands.
    if is_command(message):
        command = get_command(message)
        if command != None:
            command(message)
            return
        send_message(bot_owner_id, "🤷 Нет такой команды.")


def process_update(raw_update):
    update = json.decode(raw_update)

    if "message" in update:
        process_message(update["message"])

def hello_command(message):
    send_message(bot_owner_id, "👋 Привет!")
register_command("/hello", hello_command)

def admin_command(message):
    send_message(bot_owner_id, "🔐 Нажмите на кнопку внизу, чтобы попасть в админку бота.", {
        "inline_keyboard": [
            [
                {
                    "text": "🔑 Войти",
                    "login_url": {
                        "url": "https://starlet.onrender.com/login",
                    }
                }
            ]
        ]
    })
register_command("/admin", admin_command)

process_update(raw_update)
