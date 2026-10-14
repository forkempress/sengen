// Ponašanje koje bi inače bilo u inline atributima (CSP ih ne dozvoljava).

// Polje sa linkom za povezivanje: klik označi ceo link da se lako kopira.
document.addEventListener('click', function (e) {
  if (e.target.matches && e.target.matches('input[data-select-on-click]')) {
    e.target.select();
  }
});
