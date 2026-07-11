// registerProgressBar hooks HTMX request completion into the top progress bar UI.
export function registerProgressBar(root: HTMLElement): void {
  root.addEventListener("htmx:afterRequest", (_evt: Event) => {
    const progressBar = document.querySelector(
      ".htmx-progress-bar",
    ) as HTMLElement | null;
    if (!progressBar) return;

    // Temporarily override CSS to finish the progress bar and fade it out.
    progressBar.style.transition = "width 0.2s ease, opacity 0.4s ease 0.2s";
    progressBar.style.width = "100%";
    progressBar.style.opacity = "0";

    // Clean up inline styles after the fade out transition completes.
    setTimeout(() => {
      progressBar.style.width = "";
      progressBar.style.opacity = "";
      progressBar.style.transition = "";
    }, 600);
  });
}
