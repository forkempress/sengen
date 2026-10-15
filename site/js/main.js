// Dani90 landing: mobile menu and the floating signup / email bar. The page
// works without JavaScript too.
(function () {
  "use strict";

  var year = document.getElementById("year");
  if (year) year.textContent = String(new Date().getFullYear());

  // --- mobile menu ---
  var toggle = document.querySelector(".nav-toggle");
  var nav = document.getElementById("site-nav");
  if (toggle && nav) {
    toggle.addEventListener("click", function () {
      var open = nav.classList.toggle("open");
      toggle.setAttribute("aria-expanded", open ? "true" : "false");
    });
    nav.addEventListener("click", function (e) {
      if (e.target.tagName === "A") {
        nav.classList.remove("open");
        toggle.setAttribute("aria-expanded", "false");
      }
    });
  }

  // --- floating CTA ---
  // Phones: the bar appears once the hero buttons scroll out of view. Both
  // sizes: it steps aside over the contact section and the footer, which carry
  // their own buttons and links.
  var cta = document.getElementById("float-cta");
  if (cta && "IntersectionObserver" in window) {
    var anchor = document.querySelector("[data-cta-anchor]");
    var anchorVisible = true;
    var covered = new Set();
    var update = function () {
      cta.classList.toggle("is-visible", !anchorVisible && covered.size === 0);
      cta.classList.toggle("is-hidden", covered.size > 0);
    };
    if (anchor) {
      new IntersectionObserver(function (entries) {
        anchorVisible = entries[0].isIntersecting;
        update();
      }).observe(anchor);
    } else {
      anchorVisible = false;
    }
    var coverObs = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (e.isIntersecting) covered.add(e.target); else covered.delete(e.target);
      });
      update();
    });
    document.querySelectorAll("#kontakt, .site-footer").forEach(function (el) { coverObs.observe(el); });
    update();
  }

})();
