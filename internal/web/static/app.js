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

// Link ka sklopljenom bloku (npr. „Provera ture“) ga otvara.
document.addEventListener('click', function (e) {
  var link = e.target.closest && e.target.closest('a[data-open-details]');
  if (!link) return;
  var target = document.querySelector(link.getAttribute('href'));
  if (target && target.tagName === 'DETAILS') target.open = true;
});

// Meni „⋯“ u redu: otvoren je samo jedan, a klik van njega ili Esc ga
// zatvara.
document.addEventListener('click', function (e) {
  var inside = e.target.closest && e.target.closest('details.row-menu');
  document.querySelectorAll('details.row-menu[open]').forEach(function (d) {
    if (d !== inside) d.open = false;
  });
});
document.addEventListener('keydown', function (e) {
  if (e.key !== 'Escape') return;
  document.querySelectorAll('details.row-menu[open]').forEach(function (d) {
    d.open = false;
    d.querySelector('summary').focus();
  });
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

// Dugme za slanje pokazuje „Čuvam…“ i ne može se kliknuti dvaput (UX-35).
// Registruje se posle potvrde iznad, pa forma koju je korisnik otkazao
// ostaje netaknuta.
document.addEventListener('submit', function (e) {
  var form = e.target;
  if (e.defaultPrevented || !form.matches || form.method.toLowerCase() !== 'post') return;
  var btn = e.submitter || form.querySelector('button[type="submit"], button:not([type])');
  if (!btn || btn.disabled) return;
  btn.dataset.label = btn.textContent;
  btn.textContent = btn.getAttribute('data-busy') || 'Čuvam…';
  btn.setAttribute('aria-busy', 'true');
  // Isključuje se tek posle slanja: isključeno dugme ne bi poslalo formu.
  setTimeout(function () { btn.disabled = true; }, 0);
});

// Povratak dugmetom „nazad“ vraća stranicu iz keša sa dugmetom „Čuvam…“.
window.addEventListener('pageshow', function () {
  document.querySelectorAll('button[aria-busy="true"]').forEach(function (b) {
    b.disabled = false;
    b.removeAttribute('aria-busy');
    if (b.dataset.label) b.textContent = b.dataset.label;
  });
});

// Oko u polju za lozinku: prikaže ili sakrije lozinku. Ikonica se menja
// preko aria-pressed (CSS), fokus ostaje u polju.
document.addEventListener('click', function (e) {
  var btn = e.target.closest && e.target.closest('[data-toggle-password]');
  if (!btn) return;
  var input = document.getElementById(btn.getAttribute('data-toggle-password'));
  if (!input) return;
  var show = input.type === 'password';
  input.type = show ? 'text' : 'password';
  var label = show ? 'Sakrij lozinku' : 'Prikaži lozinku';
  btn.setAttribute('aria-label', label);
  btn.setAttribute('title', label);
  btn.setAttribute('aria-pressed', show ? 'true' : 'false');
  input.focus();
});

// Forme „Dodaj …“ stoje u dijalogu koji se otvara dugmetom. Bez JS-a
// (klasa no-js) dijalog je obična otvorena forma na stranici.
function openDialog(d) {
  if (!d || typeof d.showModal !== 'function' || d.open) return;
  d.showModal();
  var first = d.querySelector('input:not([type=hidden]), select, textarea');
  if (first) first.focus();
}

document.addEventListener('click', function (e) {
  var opener = e.target.closest && e.target.closest('[data-dialog-open]');
  if (opener) {
    var d = document.getElementById(opener.getAttribute('data-dialog-open'));
    if (d && typeof d.showModal === 'function') {
      e.preventDefault();
      openDialog(d);
    }
    return;
  }
  var closer = e.target.closest && e.target.closest('[data-dialog-close]');
  if (closer) {
    var dlg = closer.closest('dialog');
    if (dlg) dlg.close();
    return;
  }
  // Klik na zatamnjenu pozadinu zatvara dijalog (sadržaj je u .dialog-body).
  if (e.target.tagName === 'DIALOG' && e.target.open) e.target.close();
});

// Dijalog se otvara sam posle greške u formi, ili kad link vodi na njega
// (npr. /podesavanja/vozaci#dodaj-vozaca sa Pregleda).
document.addEventListener('DOMContentLoaded', function () {
  document.querySelectorAll('dialog[data-open-on-load]').forEach(openDialog);
  if (location.hash.length > 1) {
    var d = document.getElementById(location.hash.slice(1));
    if (d && d.tagName === 'DIALOG') openDialog(d);
  }
});

// Izveštaj vozača: dugme otvara štampu (u njoj i „Sačuvaj kao PDF“).
document.addEventListener('click', function (e) {
  if (e.target.closest && e.target.closest('[data-print]')) window.print();
});

// Prekidač uloge u tabeli korisnika: izbor se čuva odmah, posle potvrde.
// Ako korisnik odustane, prekidač se vraća na staru ulogu.
document.addEventListener('change', function (e) {
  var form = e.target.closest && e.target.closest('form[data-role-autosubmit]');
  if (!form || e.target.name !== 'role') return;
  var label = e.target.value === 'owner' ? 'Vlasnik' : 'Dispečer';
  var msg = form.hasAttribute('data-self')
    ? 'Promeniti vašu ulogu u „' + label + '“?' +
      (e.target.value === 'dispatcher' ? ' Više nećete moći da menjate korisnike i upozorenja.' : '')
    : 'Promeniti ulogu korisnika ' + form.getAttribute('data-user-name') + ' u „' + label + '“?';
  if (!window.confirm(msg)) {
    form.querySelectorAll('input[name="role"]').forEach(function (r) { r.checked = r.defaultChecked; });
    return;
  }
  form.setAttribute('aria-busy', 'true');
  form.submit();
});

// Datum „do“ (izlazak, povratak) ne može pre datuma „od“: kalendar se
// otvara od tog meseca, a prazno polje dobija godinu i mesec polaska, pa se
// ručno kuca samo dan. Prazan izlazak boravka (data-after bez required)
// ostaje prazan — to znači da je boravak u toku.
function syncAfter(to) {
  var from = document.getElementById(to.getAttribute('data-after'));
  if (!from || !from.value) return;
  to.min = from.value;
  if (to.required && (!to.value || to.value < from.value)) to.value = from.value;
}
document.addEventListener('change', function (e) {
  if (!e.target.id) return;
  document.querySelectorAll('input[data-after="' + e.target.id + '"]').forEach(syncAfter);
});
document.addEventListener('DOMContentLoaded', function () {
  document.querySelectorAll('input[data-after]').forEach(function (to) {
    var from = document.getElementById(to.getAttribute('data-after'));
    if (from && from.value) to.min = from.value;
  });
});

// Kalendar vozača: prekidač „Preostalo | Može na turu“ menja broj u danima
// (oba su već u stranici) i pamti se u ovom pregledaču; bez zapamćenog
// izbora važi podešavanje firme.
function setCalMode(cal, mode) {
  cal.classList.remove('cal-show-remaining', 'cal-show-trip');
  cal.classList.add('cal-show-' + mode);
  cal.querySelectorAll('[data-cal-mode]').forEach(function (b) {
    b.setAttribute('aria-pressed', b.getAttribute('data-cal-mode') === mode ? 'true' : 'false');
  });
}
document.addEventListener('click', function (e) {
  var btn = e.target.closest && e.target.closest('[data-cal-mode]');
  if (!btn) return;
  var mode = btn.getAttribute('data-cal-mode');
  setCalMode(btn.closest('.cal'), mode);
  try { localStorage.setItem('calMode', mode); } catch (err) {}
});
document.addEventListener('DOMContentLoaded', function () {
  var cal = document.querySelector('.cal');
  if (!cal) return;
  var mode = null;
  try { mode = localStorage.getItem('calMode'); } catch (err) {}
  if (mode === 'remaining' || mode === 'trip') setCalMode(cal, mode);
  var today = cal.querySelector('.day-today') || cal.querySelector('.day-cell[data-day]');
  if (today) today.setAttribute('aria-pressed', 'true');
});

// Klik na dan prikazuje stanje na taj datum u panelu pored kalendara.
function daysWord(n) {
  n = Math.abs(n);
  return n % 10 === 1 && n % 100 !== 11 ? 'dan' : 'dana';
}
document.addEventListener('click', function (e) {
  var cell = e.target.closest && e.target.closest('.cal .day-cell[data-day]');
  if (!cell) return;
  var cal = cell.closest('.cal');
  var panel = cal.querySelector('.cal-panel');
  if (!panel) return;
  cal.querySelectorAll('.day-cell[aria-pressed="true"]').forEach(function (c) { c.setAttribute('aria-pressed', 'false'); });
  cell.setAttribute('aria-pressed', 'true');

  var d = cell.dataset;
  var over = +d.over, rem = +d.rem;
  panel.querySelector('#cp-date').textContent = d.date;
  panel.querySelector('#cp-window').textContent = d.from + ' – ' + d.date;
  panel.querySelector('#cp-rem').textContent = over > 0
    ? '−' + over + ' ' + daysWord(over) + ' (preko limita)'
    : rem + ' ' + daysWord(rem);
  panel.querySelector('#cp-used').textContent = d.used + '/90';

  var future = 'future' in d, busy = 'busy' in d, trip = +(d.trip || 0);
  panel.querySelector('#cp-trip-row').hidden = !future;
  panel.querySelector('#cp-trip').textContent = busy
    ? '— već je u Šengenu tog dana'
    : trip + ' ' + daysWord(trip) + ('untilPlan' in d ? ' (do sledećeg plana)' : '');
  panel.querySelector('#cp-better').hidden = !('better' in d);
  var check = panel.querySelector('#cp-check');
  check.hidden = !future || busy || trip < 1;
  if (!check.hidden) {
    check.href = panel.getAttribute('data-driver-path') + '?od=' + d.day + '&do=' + d.tripTo + '#planovi';
  }
});
