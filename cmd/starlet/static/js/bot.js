document.addEventListener("DOMContentLoaded", () => {
  const chatMessages = document.getElementById("chat-messages");
  const messageForm = document.getElementById("message-form");
  const messageInput = document.getElementById("message-input");
  const apiCalls = document.getElementById("api-calls");

  function addMessage(text, sender) {
    const messageElement = document.createElement("div");
    messageElement.classList.add("message", `${sender}-message`);
    messageElement.textContent = text;
    chatMessages.appendChild(messageElement);
    chatMessages.scrollTop = chatMessages.scrollHeight;
  }

  messageForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    const messageText = messageInput.value.trim();
    if (messageText === "") return;

    addMessage(messageText, "user");
    messageInput.value = "";

    try {
      await fetch("/telegram", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          update_id: Date.now(),
          message: {
            message_id: Date.now(),
            date: Math.floor(Date.now() / 1000),
            chat: {
              // A mock chat ID.
              id: 12345,
              type: "private",
            },
            text: messageText,
            from: {
              // A mock user ID.
              id: 12345,
              is_bot: false,
              first_name: "Debug",
              last_name: "User",
              username: "debuguser",
            },
          },
        }),
      });
    } catch (error) {
      console.error("Error sending fake webhook:", error);
      addMessage("Error sending message. See console for details.", "bot");
    }
  });

  const eventSource = new EventSource("/debug/events");

  eventSource.onmessage = (event) => {
    const data = JSON.parse(event.data);

    const callData = JSON.stringify(data.body, null, 2);
    apiCalls.textContent += `[${new Date().toLocaleTimeString()}] ${data.method} ${data.url}\n${callData}\n\n`;
    apiCalls.scrollTop = apiCalls.scrollHeight;

    // If it's a sendMessage call, display it as a bot message.
    if (data.method === "sendMessage") {
      addMessage(data.body.text, "bot");
    }
  };

  eventSource.onerror = (error) => {
    console.error("EventSource failed:", error);
    addMessage("Connection to server lost. Please reload.", "bot");
    eventSource.close();
  };
});
