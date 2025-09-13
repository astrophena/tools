window.onload = function () {
  const maxLines = 300;
  const logContainer = document.getElementById("log-container");

  if (!logContainer) {
    console.error("Log container #log-container not found!");
    return;
  }

  function getStatusBadge(status) {
    let className = "badge-secondary";
    let text = `Status ${status}`;

    if (status >= 200 && status < 300) {
      className = "badge-success";
      text = `OK ${status}`;
    } else if (status >= 300 && status < 400) {
      className = "badge-info";
      text = `Redirect ${status}`;
    } else if (status >= 400 && status < 500) {
      className = "badge-warning";
      text = `Client Error ${status}`;
    } else if (status >= 500) {
      className = "badge-danger";
      text = `Server Error ${status}`;
    }

    return `<span class="badge ${className}">${text}</span>`;
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

        let value = log[key];
        if (key === "duration" && typeof value === "number") {
          value = `${(value / 1e9).toFixed(3)}s`;
        } else if (key === "status" && typeof value === "number") {
          dd.innerHTML = getStatusBadge(value);
          dl.appendChild(dt);
          dl.appendChild(dd);
          return;
        } else if (typeof value === "object") {
          value = JSON.stringify(value, null, 2);
        }
        dd.textContent = value;

        dl.appendChild(dt);
        dl.appendChild(dd);
      });

      attrsContainer.appendChild(dl);
      entry.appendChild(attrsContainer);
    }

    return entry;
  }

  const loadingIndicator = document.getElementById("loading-indicator");
  const loadHistoryBtn = document.getElementById("load-history-btn");

  loadHistoryBtn.addEventListener("click", async () => {
    try {
      const response = await fetch("/debug/loghistory");
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      const history = await response.json();
      history.reverse().forEach((line) => {
        try {
          const logData = JSON.parse(line);
          const logElement = createLogElement(logData);
          logContainer.insertBefore(logElement, logContainer.firstChild);
        } catch (error) {
          console.error("Failed to parse log line from history:", line, error);
        }
      });
      loadHistoryBtn.style.display = "none";
    } catch (error) {
      console.error("Failed to load log history:", error);
    }
  });

  const eventSource = new EventSource("/debug/log", { withCredentials: true });

  eventSource.addEventListener("logline", function (e) {
    if (loadingIndicator) {
      loadingIndicator.style.display = "none";
    }

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
