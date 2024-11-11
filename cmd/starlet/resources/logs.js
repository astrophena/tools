window.onload = function() {
  const maxLines = 300;
  new EventSource("/debug/log", { withCredentials: true }).addEventListener("logline", function(e) {
    // Append line to whatever is in the pre block. Then, truncate number of lines to maxLines.
    // This is extremely inefficient, since we're splitting into component lines and joining them
    // back each time a line is added.
    var txt = document.getElementById("logs").innerText + e.data + "\n";
    document.getElementById("logs").innerText = txt.split('\n').slice(-maxLines).join('\n');
  });
}
