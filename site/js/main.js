// Dani90 landing: mobile menu and the demo-request form. The page works
// without JavaScript too — the form then posts straight to Web3Forms and
// lands on /hvala.html.
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

  // --- demo form ---
  var form = document.getElementById("demo-form");
  var status = document.getElementById("form-status");
  if (!form || !status) return;

  function setStatus(text, kind) {
    status.textContent = text;
    status.className = "form-status " + (kind || "");
  }

  form.addEventListener("submit", function (e) {
    e.preventDefault();

    // Until the real Web3Forms key is filled in, say so instead of letting
    // requests silently disappear.
    if (form.elements.access_key.value === "WEB3FORMS_ACCESS_KEY") {
      setStatus("Forma još nije povezana — pozovite nas ili pišite direktno.", "err");
      return;
    }

    var missing = null;
    ["ime", "firma", "telefon"].forEach(function (name) {
      var el = form.elements[name];
      var empty = !el.value.trim();
      el.setAttribute("aria-invalid", empty ? "true" : "false");
      if (empty && !missing) missing = el;
    });
    var email = form.elements.email;
    var badEmail = email.value.trim() !== "" && !email.checkValidity();
    email.setAttribute("aria-invalid", badEmail ? "true" : "false");
    if (missing || badEmail) {
      setStatus(missing ? "Popunite ime, firmu i telefon." : "Email adresa nije ispravna.", "err");
      (missing || email).focus();
      return;
    }

    var button = form.querySelector("button[type=submit]");
    button.disabled = true;
    setStatus("Šaljem…");

    var data = new FormData(form);
    data.delete("redirect"); // the JS path shows its own confirmation

    fetch(form.action, { method: "POST", body: data, headers: { Accept: "application/json" } })
      .then(function (res) { return res.json().then(function (body) { return { ok: res.ok, body: body }; }); })
      .then(function (r) {
        if (r.ok && r.body && r.body.success) {
          form.reset();
          setStatus("Hvala! Javićemo vam se uskoro.", "ok");
        } else {
          setStatus("Slanje nije uspelo. Pokušajte ponovo ili nas pozovite.", "err");
        }
      })
      .catch(function () {
        setStatus("Nema veze sa internetom. Pokušajte ponovo.", "err");
      })
      .finally(function () { button.disabled = false; });
  });
})();
