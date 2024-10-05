result = gemini.generate_content(
    model = "gemini-1.5-flash",
    contents = ["Write a story about very kind heiress and her maid."],
    system = {
        "text": "Write in Russian."
    }
)

print(result[0][0])
