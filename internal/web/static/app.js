// Ponašanje koje bi inače bilo u inline atributima (CSP ih ne dozvoljava).

// Bez JS-a meni je uvek otvoren (klasa no-js); sa JS-om se na telefonu
// otvara dugmetom „Meni“.
document.documentElement.classList.remove('no-js');

document.addEventListener('click', function (e) {
  var toggle = e.target.closest && e.target.closest('.nav-toggle');
  if (!toggle) return;
  var header = toggle.closest('header');
  var open = header.classList.toggle('nav-open');
  toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
});

// Na telefonu se sklapaju blokovi koji na desktopu stoje otvoreni
// (npr. raniji meseci kalendara).
document.addEventListener('DOMContentLoaded', function () {
  if (window.matchMedia('(max-width: 700px)').matches) {
    document.querySelectorAll('details[data-close-on-mobile]').forEach(function (d) { d.open = false; });
  }
});

// Link ka sklopljenom bloku (npr. „Provera ture“) ga otvara.
document.addEventListener('click', function (e) {
  var link = e.target.closest && e.target.closest('a[data-open-details]');
  if (!link) return;
  var target = document.querySelector(link.getAttribute('href'));
  if (target && target.tagName === 'DETAILS') target.open = true;
});

// Polje sa linkom za povezivanje: klik označi ceo link da se lako kopira.
document.addEventListener('click', function (e) {
  if (e.target.matches && e.target.matches('input[data-select-on-click]')) {
    e.target.select();
  }
});

// Forme sa data-confirm (brisanje boravka, deaktivacija) pitaju pre slanja.
// hx-confirm radi samo za htmx zahteve, a ovo su obične forme.
document.addEventListener('submit', function (e) {
  var form = e.target;
  var msg = form.getAttribute && form.getAttribute('data-confirm');
  if (msg && !window.confirm(msg)) {
    e.preventDefault();
  }
});
