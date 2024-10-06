#!/usr/bin/env python

from argparse import ArgumentParser
from json import load, dump

models = {
    "models/gemini-1.5-pro": "gemini-1.5-pro-001",
    "models/gemini-1.5-flash": "gemini-1.5-flash-001",
}

parser = ArgumentParser(
    description="Partially converts Google AI Studio prompt file to Vertex AI format."
)
parser.add_argument("src", type=str)
parser.add_argument("dst", type=str)
args = parser.parse_args()

with open(args.src) as file:
    prompt = load(file)

converted = {
    "title": "Google AI Studio import ({src})".format(src=args.src),
    "parameters": {
        "temperature": prompt["runSettings"]["temperature"],
        "tokenLimits": prompt["runSettings"]["maxOutputTokens"],
        "topP": prompt["runSettings"]["topP"],
    },
    "type": "multimodal_chat",
    "messages": [],
    "model": models.get(prompt["runSettings"]["model"], "gemini-1.5-pro"),
}

for message in prompt["chunkedPrompt"]["chunks"]:
    content = {"parts": [{"text": message["text"]}]}
    if message["role"] == "model":
        content["role"] = "model"
    converted["messages"].append(
        {
            "author": "user" if message["role"] == "user" else "bot",
            "content": content,
        }
    )

with open(args.dst, "w") as file:
    dump(converted, file, indent=2)
