document.addEventListener("DOMContentLoaded", () => {
  const tableBody = document.getElementById("stats-table-body");
  if (!tableBody) {
    console.error("Stats table body not found.");
    return;
  }

  const eventSource = new EventSource("/debug/gemini-proxy-stats-stream");

  eventSource.onmessage = (event) => {
    try {
      const stats = JSON.parse(event.data);
      updateTable(stats);
    } catch (error) {
      console.error("Failed to parse stats data:", error);
    }
  };

  eventSource.onerror = (error) => {
    console.error("EventSource failed:", error);
    const row = tableBody.insertRow();
    const cell = row.insertCell();
    cell.colSpan = 5;
    cell.textContent = "Connection to server lost. Please reload.";
    eventSource.close();
  };

  function updateTable(stats) {
    // Clear existing rows.
    tableBody.innerHTML = "";

    if (stats === null || stats.length === 0) {
      const row = tableBody.insertRow();
      const cell = row.insertCell();
      cell.colSpan = 5;
      cell.textContent = "No token usage data available yet.";
      return;
    }

    stats.forEach((stat) => {
      const row = tableBody.insertRow();
      row.insertCell().textContent = stat.id;
      row.insertCell().textContent = stat.description;
      row.insertCell().textContent = stat.requests;
      row.insertCell().textContent = stat.limit;
      row.insertCell().textContent = stat.last_used;
    });
  }
});
