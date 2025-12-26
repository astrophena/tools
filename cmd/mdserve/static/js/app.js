/**
 * @class Enhancer
 * @description Adds dynamic features like a TOC and other UI improvements.
 */
class Enhancer {
  constructor() {
    this.mainContent = null;
    this.headings = [];
    this.userInitiatedScroll = false; // flag to prevent observer conflicts
    this.scrollTimeout = null;
  }

  /**
   * Initializes the enhancer.
   * Finds the main content, then generates all features.
   * @public
   */
  init() {
    const content = Array.from(document.body.childNodes);
    this.mainContent = document.createElement("div");
    this.mainContent.id = "main-content";
    content.forEach((node) => this.mainContent.appendChild(node));
    document.body.appendChild(this.mainContent);

    this.headings = Array.from(this.mainContent.querySelectorAll("h2"));
    if (this.headings.length > 1) {
      this.createTOC();
      this.setupIntersectionObserver();
    }

    this.createBackToTopButton();
    this.setupScrollListener(); // set up the scroll end detector
    this.setupImageEnlarger();
  }

  /**
   * Creates and injects the Table of Contents into the page.
   * @private
   */
  createTOC() {
    const tocContainer = document.createElement("div");
    tocContainer.id = "toc-container";

    if (window.innerWidth <= 1200) {
      tocContainer.classList.add("collapsed");
    }

    const tocList = document.createElement("ul");
    tocList.id = "toc-list";

    this.headings.forEach((heading, index) => {
      const text = heading.textContent;
      const id = this.slugify(text) || `section-${index}`;
      heading.id = id;
      const listItem = document.createElement("li");
      const link = document.createElement("a");
      link.href = `#${id}`;
      link.textContent = text;
      link.addEventListener("click", (e) => this.smoothScroll(e, id));
      listItem.appendChild(link);
      tocList.appendChild(listItem);
    });

    tocContainer.innerHTML = `
      <div id="toc-header">
        <h3>Table of contents</h3>
        <button id="toc-toggle" title="Hide/show table of contents">×</button>
      </div>
    `;
    tocContainer.appendChild(tocList);
    document.body.prepend(tocContainer);

    const outsideToggle = document.createElement("button");
    outsideToggle.id = "toc-toggle-outside";
    outsideToggle.innerHTML = "☰";
    document.body.prepend(outsideToggle);

    const tocToggle = document.getElementById("toc-toggle");
    tocToggle.addEventListener("click", () => {
      tocContainer.classList.add("collapsed");
      tocContainer.classList.remove("visible");
      outsideToggle.classList.remove("hidden");
    });

    outsideToggle.addEventListener("click", () => {
      tocContainer.classList.add("visible");
      tocContainer.classList.remove("collapsed");
      outsideToggle.classList.add("hidden");
    });
  }

  /**
   * Creates and manages the "Back to Top" button with animation.
   * @private
   */
  createBackToTopButton() {
    const button = document.createElement("button");
    button.id = "back-to-top";
    button.className = "ui-button";
    button.innerHTML = "&uarr;";
    button.title = "Up";
    document.body.appendChild(button);

    button.addEventListener(
      "click",
      () => window.scrollTo({ top: 0, behavior: "smooth" }),
    );

    window.addEventListener("scroll", () => {
      button.classList.toggle("visible", window.scrollY > 300);
    });
  }

  /**
   * Sets up an Intersection Observer to highlight the current section in the TOC.
   * @private
   */
  setupIntersectionObserver() {
    let intersectingEntries = [];

    const observer = new IntersectionObserver(
      (entries) => {
        // Guard clause to prevent observer from firing during user-initiated scroll.
        if (this.userInitiatedScroll) return;

        entries.forEach((entry) => {
          const index = intersectingEntries.findIndex(
            (target) => target === entry.target,
          );
          if (entry.isIntersecting && index === -1) {
            intersectingEntries.push(entry.target);
          } else if (!entry.isIntersecting && index > -1) {
            intersectingEntries.splice(index, 1);
          }
        });

        document
          .querySelectorAll("#toc-list a.active")
          .forEach((activeLink) => activeLink.classList.remove("active"));

        if (intersectingEntries.length > 0) {
          const topmostElement = intersectingEntries.reduce((a, b) =>
            a.getBoundingClientRect().top < b.getBoundingClientRect().top
              ? a
              : b
          );
          const id = topmostElement.getAttribute("id");
          const link = document.querySelector(`#toc-list a[href="#${id}"]`);
          if (link) {
            link.classList.add("active");
          }
        }
      },
      {
        rootMargin: "-20px 0px -70% 0px",
      },
    );

    this.headings.forEach((heading) => observer.observe(heading));
  }

  /**
   * Handles smooth scrolling for anchor links and manages the scroll flag.
   * @private
   */
  smoothScroll(e, id) {
    e.preventDefault();
    this.userInitiatedScroll = true; // set the flag before scrolling

    // Immediately update active class for instant feedback.
    document
      .querySelectorAll("#toc-list a.active")
      .forEach((link) => link.classList.remove("active"));
    e.target.classList.add("active");

    document.getElementById(id).scrollIntoView({ behavior: "smooth" });

    // The universal scroll listener will reset the flag when scrolling stops.
  }

  /**
   * Sets up a debounced listener to detect when any scrolling has stopped.
   * @private
   */
  setupScrollListener() {
    window.addEventListener("scroll", () => {
      clearTimeout(this.scrollTimeout);
      this.scrollTimeout = setTimeout(() => {
        this.userInitiatedScroll = false;
      }, 150); // A 150ms delay is enough to detect scroll end.
    });
  }

  /**
   * Generates a URL-friendly slug from a string.
   * @private
   */
  slugify(text) {
    return text
      .toString()
      .toLowerCase()
      .replace(/\s+/g, "-")
      .replace(/[^\w\-]+/g, "")
      .replace(/\-\-+/g, "-")
      .replace(/^-+/, "")
      .replace(/-+$/, "");
  }

  /**
   * Sets up the image enlarger.
   * @private
   */
  setupImageEnlarger() {
    const images = this.mainContent.querySelectorAll("img");
    images.forEach((img) => {
      img.addEventListener("click", () => this.enlargeImage(img));
    });
  }

  /**
   * Creates and manages the image enlarger overlay.
   * @private
   */
  enlargeImage(img) {
    const overlay = document.createElement("div");
    overlay.id = "image-overlay";
    const enlargedImg = document.createElement("img");
    enlargedImg.src = img.src;

    overlay.appendChild(enlargedImg);
    document.body.appendChild(overlay);

    setTimeout(() => {
      overlay.classList.add("visible");
    }, 10);

    const closeOverlay = () => {
      overlay.classList.remove("visible");
      setTimeout(() => {
        document.body.removeChild(overlay);
        document.removeEventListener("keydown", onKeyDown);
      }, 200);
    };

    const onKeyDown = (e) => {
      if (e.key === "Escape") {
        closeOverlay();
      }
    };

    overlay.addEventListener("click", closeOverlay);
    document.addEventListener("keydown", onKeyDown);
  }
}

// Run the script after the DOM is fully loaded.
document.addEventListener("DOMContentLoaded", () => {
  new Enhancer().init();
});
