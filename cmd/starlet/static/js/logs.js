window.onload = function () {
  const maxLines = 300;
  const logContainer = document.getElementById("log-container");

  if (!logContainer) {
    console.error("Log container #log-container not found!");
    return;
  }

  function createLogElement(log) {
    const entry = document.createElement("div");
    entry.className = "log-entry";

    const time = document.createElement("span");
    time.className = "log-time";
    time.textContent = new Date(log.time).toLocaleTimeString();
    entry.appendChild(time);

    const level = document.createElement("span");
    const levelClass = `log-level-${log.level.toLowerCase()}`;
    level.className = `log-level ${levelClass || "log-level-default"}`;
    level.textContent = log.level;
    entry.appendChild(level);

    const msg = document.createElement("span");
    msg.className = "log-msg";
    msg.textContent = log.msg;
    entry.appendChild(msg);

    const attrs = Object.keys(log).filter(
      (key) => !["time", "level", "msg"].includes(key),
    );
    if (attrs.length > 0) {
      const attrsContainer = document.createElement("div");
      attrsContainer.className = "log-attrs";
      const dl = document.createElement("dl");

      attrs.forEach((key) => {
        const dt = document.createElement("dt");
        dt.textContent = key;
        const dd = document.createElement("dd");
        const value =
          typeof log[key] === "object"
            ? JSON.stringify(log[key], null, 2)
            : log[key];
        dd.textContent = value;
        dl.appendChild(dt);
        dl.appendChild(dd);
      });

      attrsContainer.appendChild(dl);
      entry.appendChild(attrsContainer);
    }

    return entry;
  }

  const initialLogsRaw = logContainer.textContent.trim();
  logContainer.innerHTML = "";

  if (initialLogsRaw) {
    initialLogsRaw.split("\n").forEach((line) => {
      try {
        const log = JSON.parse(line);
        logContainer.appendChild(createLogElement(log));
      } catch (e) {
        const pre = document.createElement("pre");
        pre.textContent = line;
        logContainer.appendChild(pre);
      }
    });
  }

  const eventSource = new EventSource("/debug/log", { withCredentials: true });
  eventSource.addEventListener("logline", function (e) {
    try {
      const logData = JSON.parse(e.data);
      const logElement = createLogElement(logData);
      logContainer.appendChild(logElement);

      while (logContainer.children.length > maxLines) {
        logContainer.removeChild(logContainer.firstChild);
      }

      logContainer.scrollTop = logContainer.scrollHeight;
    } catch (error) {
      console.error("Failed to parse log line:", e.data, error);
    }
  });
};
