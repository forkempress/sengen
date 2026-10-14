// Dani90 — pravilo 90/180 za javni kalkulator na sajtu. Prevod funkcija iz
// internal/calc/calc.go (iste definicije, isti rezultati), bez I/O.
//
// Datum je ceo broj: broj dana od 1.1.1970. Tako je računanje razlike tačno,
// bez sati i vremenskih zona. Boravak je {entry, exit}; exit === null znači
// da je osoba još u Šengenu, pa se boravak računa do dana koji se proverava.
(function (root) {
  "use strict";

  var WINDOW_DAYS = 180;
  var MAX_DAYS = 90;
  var LIMIT = 3650; // sigurnosni ventil za petlje, kao u Go kodu

  // usedDays: broj dana u Šengenu u prozoru [d-179, d].
  // Dan se broji jednom i kad se boravci dodiruju ili preklapaju (dva
  // prelaza istog dana), pa se dani obeležavaju umesto da se sabiraju.
  function usedDays(stays, d) {
    var winStart = d - (WINDOW_DAYS - 1);
    var inWindow = new Array(WINDOW_DAYS);
    for (var i = 0; i < stays.length; i++) {
      var s = stays[i];
      if (s.entry > d) continue;
      var exit = s.exit === null ? d : s.exit;
      var start = Math.max(s.entry, winStart);
      var end = Math.min(exit, d);
      for (var x = start; x <= end; x++) inWindow[x - winStart] = true;
    }
    var total = 0;
    for (var j = 0; j < WINDOW_DAYS; j++) if (inWindow[j]) total++;
    return total;
  }

  function remaining(stays, today) {
    return Math.max(0, MAX_DAYS - usedDays(stays, today));
  }

  function openStay(stays) {
    for (var i = 0; i < stays.length; i++) {
      if (stays[i].exit === null) return stays[i];
    }
    return null;
  }

  // maxContinuousUntil: poslednji dan do kog osoba koja je sada u Šengenu
  // sme da ostane bez prekida. Ako je već preko limita, vraća poslednji dan
  // ovog boravka na koji je još bila u limitu (datum pre danas). null ako
  // nije u Šengenu.
  function maxContinuousUntil(stays, today) {
    var open = openStay(stays);
    if (!open) return null;
    if (usedDays(stays, today) > MAX_DAYS) {
      for (var b = today - 1; b >= open.entry; b--) {
        if (usedDays(stays, b) <= MAX_DAYS) return b;
      }
      return open.entry - 1;
    }
    // Otvoreni boravak se u usedDays računa do dana provere, pa je dovoljno
    // pitati za svaki sledeći dan.
    var last = today;
    for (var d = today + 1; d - today <= LIMIT; d++) {
      if (usedDays(stays, d) > MAX_DAYS) break;
      last = d;
    }
    return last;
  }

  // maxTripLength: najduža neprekidna tura koja počinje na dan start.
  function maxTripLength(stays, start) {
    var length = 0;
    for (var d = start; length <= LIMIT; d++) {
      var trial = stays.concat([{ entry: start, exit: d }]);
      if (usedDays(trial, d) > MAX_DAYS) break;
      length++;
    }
    return length;
  }

  // canDoTrip: da li tura od length dana od start nijednog dana ne prelazi 90.
  function canDoTrip(stays, start, length) {
    var peak = 0;
    var firstOverstay = null;
    for (var i = 0; i < length; i++) {
      var d = start + i;
      var used = usedDays(stays.concat([{ entry: start, exit: d }]), d);
      if (used > peak) peak = used;
      if (used > MAX_DAYS && firstOverstay === null) firstOverstay = d;
    }
    return { ok: firstOverstay === null, peakUsed: peak, firstOverstay: firstOverstay };
  }

  // earliestStart: prvi dan od from kad tura od length dana može da počne.
  function earliestStart(stays, length, from) {
    for (var i = 0; i < LIMIT; i++) {
      if (canDoTrip(stays, from + i, length).ok) return from + i;
    }
    return null;
  }

  function closeOpenAt(stays, today) {
    return stays.map(function (s) {
      return s.exit === null ? { entry: s.entry, exit: today } : s;
    });
  }

  // freeUpSchedule: dani u (today, today+horizon] kad se dani vraćaju, uz
  // pretpostavku da osoba izlazi danas i ne putuje više.
  function freeUpSchedule(stays, today, horizon) {
    var closed = closeOpenAt(stays, today);
    var events = [];
    var prev = remaining(closed, today);
    for (var i = 1; i <= horizon; i++) {
      var r = remaining(closed, today + i);
      if (r > prev) events.push({ date: today + i, daysFreed: r - prev });
      prev = r;
    }
    return events;
  }

  // --- datumi ---

  var MS_PER_DAY = 86400000;

  // fromISO("2026-10-14") -> broj dana; null ako datum nije ispravan.
  function fromISO(s) {
    var m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s || "");
    if (!m) return null;
    var y = +m[1], mo = +m[2], d = +m[3];
    var t = Date.UTC(y, mo - 1, d);
    var back = new Date(t);
    if (back.getUTCFullYear() !== y || back.getUTCMonth() !== mo - 1 || back.getUTCDate() !== d) return null;
    return t / MS_PER_DAY;
  }

  function toISO(day) {
    return new Date(day * MS_PER_DAY).toISOString().slice(0, 10);
  }

  // format(day) -> "14.10.2026", kao u aplikaciji.
  function format(day) {
    var p = toISO(day).split("-");
    return p[2] + "." + p[1] + "." + p[0];
  }

  // todayBelgrade: današnji datum u Srbiji, bez obzira na zonu računara.
  function todayBelgrade() {
    var parts = new Intl.DateTimeFormat("en-CA", {
      timeZone: "Europe/Belgrade", year: "numeric", month: "2-digit", day: "2-digit"
    }).format(new Date());
    return fromISO(parts);
  }

  var api = {
    WINDOW_DAYS: WINDOW_DAYS,
    MAX_DAYS: MAX_DAYS,
    usedDays: usedDays,
    remaining: remaining,
    maxContinuousUntil: maxContinuousUntil,
    maxTripLength: maxTripLength,
    canDoTrip: canDoTrip,
    earliestStart: earliestStart,
    freeUpSchedule: freeUpSchedule,
    fromISO: fromISO,
    toISO: toISO,
    format: format,
    todayBelgrade: todayBelgrade
  };

  if (typeof module === "object" && module.exports) module.exports = api;
  else root.Calc90 = api;
})(this);
