const dialog = document.querySelector("#image-dialog");
const preview = dialog.querySelector("img");

// The native dialog handles focus and Escape; clicking outside also closes it.
document.querySelector("main").addEventListener("click", (event) => {
  if (!(event.target instanceof HTMLImageElement)) return;
  preview.src = event.target.currentSrc || event.target.src;
  preview.alt = event.target.alt;
  dialog.showModal();
});

dialog.addEventListener("click", (event) => {
  if (event.target === dialog) dialog.close();
});
