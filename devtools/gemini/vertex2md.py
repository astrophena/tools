#!/usr/bin/env python

from argparse import ArgumentParser
from json import load
from textwrap import indent

parser = ArgumentParser(description="Converts Vertex AI chat prompt JSON to Markdown.")
parser.add_argument("src", type=str)
parser.add_argument("dst", type=str)
args = parser.parse_args()

with open(args.src) as file:
    prompt = load(file)

with open(args.dst, "w") as file:
    print("# {title}".format(title=prompt["title"]), file=file)
    print("", file=file)
    for message in prompt["messages"]:
        text = message["content"]["parts"][0]["text"]
        if message["author"] == "user":
            text = indent(text, "> ", lambda line: True)
            text += "\n"
        print(text, file=file)
