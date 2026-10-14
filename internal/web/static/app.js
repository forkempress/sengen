// Ponašanje koje bi inače bilo u inline atributima (CSP ih ne dozvoljava).

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
