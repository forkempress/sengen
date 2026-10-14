// Dani90 — javni kalkulator 90/180. Računica je u calc90.js; ovde su samo
// unos, provera unosa i prikaz. Ništa se ne šalje na server: unos se pamti
// samo u pregledaču (localStorage), i stranica radi i kad to nije dostupno.
(function () {
  "use strict";

  var C = window.Calc90;
  var form = document.getElementById("calc-form");
  var list = document.getElementById("stays");
  var out = document.getElementById("calc-result");
  if (!C || !form || !list || !out) return;

  var STORAGE_KEY = "dani90.kalkulator.v1";
  var planStart = document.getElementById("plan-start");
  var planLen = document.getElementById("plan-len");

  var today = C.todayBelgrade();
  var todayISO = C.toISO(today);

  // --- stanje i čuvanje ---

  function load() {
    try {
      var raw = window.localStorage.getItem(STORAGE_KEY);
      if (!raw) return null;
      var s = JSON.parse(raw);
      if (!s || !Array.isArray(s.stays)) return null;
      return s;
    } catch (e) {
      return null;
    }
  }

  function save() {
    try {
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify({
        stays: readRows().map(function (r) { return { entry: r.entry, exit: r.exit }; }),
        planStart: planStart.value,
        planLen: planLen.value
      }));
    } catch (e) { /* privatni prozor ili blokiran storage — samo ne pamtimo */ }
  }

  // --- redovi sa boravcima ---

  var rowSeq = 0;

  function addRow(entry, exit) {
    rowSeq++;
    var li = document.createElement("li");
    li.className = "stay";

    li.appendChild(dateField("Ulazak", "in-" + rowSeq, entry, "entry"));
    li.appendChild(dateField("Izlazak", "out-" + rowSeq, exit, "exit"));

    var del = document.createElement("button");
    del.type = "button";
    del.className = "stay-del";
    del.setAttribute("aria-label", "Ukloni boravak");
    del.textContent = "×";
    del.addEventListener("click", function () {
      li.remove();
      if (!list.children.length) addRow("", "");
      update();
    });
    li.appendChild(del);

    var err = document.createElement("p");
    err.className = "stay-err";
    err.setAttribute("role", "alert");
    li.appendChild(err);

    list.appendChild(li);
    return li;
  }

  function dateField(label, id, value, role) {
    var wrap = document.createElement("div");
    wrap.className = "field";
    var l = document.createElement("label");
    l.htmlFor = id;
    l.textContent = label;
    var input = document.createElement("input");
    input.type = "date";
    input.id = id;
    input.max = todayISO;
    input.value = value || "";
    input.dataset.role = role;
    wrap.appendChild(l);
    wrap.appendChild(input);
    return wrap;
  }

  function readRows() {
    return Array.prototype.map.call(list.children, function (li) {
      return {
        li: li,
        entry: li.querySelector('[data-role="entry"]').value,
        exit: li.querySelector('[data-role="exit"]').value
      };
    });
  }

  // validate vraća boravke za računicu i upisuje grešku uz svaki loš red.
  function validate(rows) {
    var stays = [];
    var errors = 0;

    rows.forEach(function (r) {
      r.msg = "";
      if (!r.entry && !r.exit) return; // prazan red se ne računa
      var entry = C.fromISO(r.entry);
      var exit = r.exit ? C.fromISO(r.exit) : null;
      if (entry === null) r.msg = "Upišite datum ulaska.";
      else if (r.exit && exit === null) r.msg = "Datum izlaska nije ispravan.";
      else if (entry > today) r.msg = "Ulazak je u budućnosti. Buduću turu proverite u delu „Planirana tura“.";
      else if (exit !== null && exit < entry) r.msg = "Izlazak je pre ulaska.";
      else if (exit !== null && exit > today) r.msg = "Izlazak je u budućnosti. Ako ste još u Šengenu, ostavite izlazak prazan.";
      else r.stay = { entry: entry, exit: exit };
    });

    var valid = rows.filter(function (r) { return r.stay; });

    var open = valid.filter(function (r) { return r.stay.exit === null; });
    if (open.length > 1) {
      open.forEach(function (r) { r.msg = "Samo jedan boravak može biti bez izlaska — onaj u kom ste sada."; });
    }

    // Preklapanje bi iste dane brojalo dvaput. Otvoren boravak traje do danas.
    for (var i = 0; i < valid.length; i++) {
      for (var j = i + 1; j < valid.length; j++) {
        var a = valid[i].stay, b = valid[j].stay;
        var aEnd = a.exit === null ? today : a.exit;
        var bEnd = b.exit === null ? today : b.exit;
        if (a.entry <= bEnd && b.entry <= aEnd) {
          if (!valid[i].msg) valid[i].msg = "Preklapa se sa drugim boravkom.";
          if (!valid[j].msg) valid[j].msg = "Preklapa se sa drugim boravkom.";
        }
      }
    }

    rows.forEach(function (r) {
      var err = r.li.querySelector(".stay-err");
      err.textContent = r.msg;
      r.li.classList.toggle("has-err", !!r.msg);
      if (r.msg) errors++;
      else if (r.stay) stays.push(r.stay);
    });

    return { stays: stays, errors: errors };
  }

  // --- prikaz rezultata ---

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  function daysWord(n) {
    var m10 = n % 10, m100 = n % 100;
    if (m10 === 1 && m100 !== 11) return n + " dan";
    return n + " dana";
  }

  function level(rem) {
    if (rem < 15) return "red";
    if (rem <= 30) return "orange";
    return "green";
  }

  function render(v) {
    out.textContent = "";

    if (v.errors) {
      out.appendChild(el("p", "calc-empty", "Ispravite označene redove i rezultat će se pojaviti ovde."));
      return;
    }

    var stays = v.stays;
    var used = C.usedDays(stays, today);
    var rem = C.remaining(stays, today);
    var isIn = stays.some(function (s) { return s.exit === null; });

    var card = el("div", "result-card");
    card.appendChild(el("p", "result-date", "Stanje na dan " + C.format(today)));

    var nums = el("div", "result-nums");
    var u = el("div", "result-num");
    u.appendChild(el("span", "result-label", "Iskorišćeno"));
    u.appendChild(el("strong", used > C.MAX_DAYS ? "is-red" : "", used + "/" + C.MAX_DAYS));
    var r = el("div", "result-num");
    r.appendChild(el("span", "result-label", "Preostalo danas"));
    r.appendChild(el("strong", "is-" + level(rem), String(rem)));
    nums.appendChild(u);
    nums.appendChild(r);
    card.appendChild(nums);

    // Glavni odgovor: do kad sme da ostane, ili kolika tura sme od sutra.
    var main = el("p", "result-main");
    if (isIn) {
      var until = C.maxContinuousUntil(stays, today);
      if (until < today) {
        main.classList.add("is-alert");
        main.appendChild(document.createTextNode("Preko limita za " + daysWord(used - C.MAX_DAYS) + ". Trebalo je izaći do "));
        main.appendChild(el("b", "", C.format(until)));
        main.appendChild(document.createTextNode("."));
      } else {
        main.appendChild(document.createTextNode("Sme da ostane bez prekida do "));
        main.appendChild(el("b", "", C.format(until)));
        main.appendChild(document.createTextNode(until > today ? " (još " + daysWord(until - today) + ")." : " — danas je poslednji dan."));
      }
    } else if (stays.length) {
      var longest = C.maxTripLength(stays, today + 1);
      if (longest > 0) {
        main.appendChild(document.createTextNode("Najduža tura od sutra: "));
        main.appendChild(el("b", "", daysWord(longest)));
        main.appendChild(document.createTextNode("."));
      } else {
        var first = C.earliestStart(stays, 1, today + 1);
        main.classList.add("is-alert");
        if (used > C.MAX_DAYS) {
          main.appendChild(document.createTextNode("Preko limita za " + daysWord(used - C.MAX_DAYS) + ". "));
        }
        main.appendChild(document.createTextNode("Sutra ne sme da uđe. Prvi dan kad sme: "));
        main.appendChild(el("b", "", C.format(first)));
        main.appendChild(document.createTextNode("."));
      }
    } else {
      main.appendChild(document.createTextNode("Bez boravaka u poslednjih 180 dana: na raspolaganju je svih 90 dana."));
    }
    card.appendChild(main);

    card.appendChild(strip(stays));
    out.appendChild(card);

    var plan = planResult(stays, isIn);
    if (plan) out.appendChild(plan);

    var events = C.freeUpSchedule(stays, today, C.WINDOW_DAYS).slice(0, 6);
    if (events.length) {
      var fu = el("div", "result-card");
      fu.appendChild(el("h3", "result-h", isIn ? "Kad se dani vraćaju (ako izađete danas)" : "Kad se dani vraćaju (ako ne putujete)"));
      var ul = el("ul", "freeup");
      var total = rem;
      events.forEach(function (e) {
        total += e.daysFreed;
        var li = el("li");
        li.appendChild(el("span", "freeup-date", C.format(e.date)));
        li.appendChild(el("span", "freeup-plus", "+" + e.daysFreed));
        li.appendChild(el("span", "freeup-total", "ukupno " + Math.min(total, C.MAX_DAYS)));
        ul.appendChild(li);
      });
      fu.appendChild(ul);
      out.appendChild(fu);
    }
  }

  // strip: poslednjih 180 dana, svaki dan jedan kvadratić.
  function strip(stays) {
    var wrap = el("div", "strip-wrap");
    var start = today - (C.WINDOW_DAYS - 1);
    var used = C.usedDays(stays, today);
    var grid = el("div", "strip");
    grid.setAttribute("role", "img");
    grid.setAttribute("aria-label", "Poslednjih 180 dana: " + used + " dana u Šengenu.");
    for (var d = start; d <= today; d++) {
      var inS = stays.some(function (s) {
        var end = s.exit === null ? today : s.exit;
        return s.entry <= d && d <= end;
      });
      grid.appendChild(el("span", inS ? "on" : ""));
    }
    wrap.appendChild(grid);
    var legend = el("p", "strip-legend");
    legend.appendChild(el("span", "", C.format(start)));
    legend.appendChild(el("span", "strip-key", "■ u Šengenu"));
    legend.appendChild(el("span", "", "danas"));
    wrap.appendChild(legend);
    return wrap;
  }

  function planResult(stays, isIn) {
    var start = C.fromISO(planStart.value);
    if (start === null) return null;

    var card = el("div", "result-card");
    card.appendChild(el("h3", "result-h", "Planirana tura od " + C.format(start)));

    var minStart = isIn ? today + 1 : today;
    if (start < minStart) {
      card.appendChild(el("p", "result-note", isIn
        ? "Polazak mora biti posle danas — sada ste u Šengenu."
        : "Polazak ne može biti u prošlosti."));
      return card;
    }

    // Ako je osoba sada u Šengenu, računa se kao da izlazi danas.
    var closed = stays.map(function (s) { return s.exit === null ? { entry: s.entry, exit: today } : s; });
    var longest = C.maxTripLength(closed, start);

    var lenRaw = planLen.value.trim();
    var len = parseInt(lenRaw, 10);
    if (lenRaw && (!(len >= 1) || len > C.MAX_DAYS || String(len) !== lenRaw)) {
      card.appendChild(el("p", "result-note", "Trajanje upišite kao broj dana, od 1 do 90."));
    } else if (lenRaw) {
      var check = C.canDoTrip(closed, start, len);
      var p = el("p", "result-main " + (check.ok ? "is-ok" : "is-alert"));
      if (check.ok) {
        p.textContent = "Tura od " + daysWord(len) + " može. Najviše iskorišćenih tokom ture: " + check.peakUsed + "/90.";
      } else {
        var earliest = C.earliestStart(closed, len, start);
        p.textContent = "Tura od " + daysWord(len) + " ne može: limit se prelazi " + C.format(check.firstOverstay) +
          ". Najraniji polazak za ovu turu: " + C.format(earliest) + ".";
      }
      card.appendChild(p);
    }

    card.appendChild(el("p", "result-note", "Najduža tura sa ovim polaskom: " + daysWord(longest) +
      (longest ? " (do " + C.format(start + longest - 1) + ")." : ".")));
    if (isIn) card.appendChild(el("p", "result-note muted", "Računa se kao da izlazite iz Šengena danas."));
    return card;
  }

  // --- primer i dugmad ---

  function example() {
    // Primer je vezan za današnji dan, da uvek izgleda kao sveža istorija.
    var spans = [[170, 160], [140, 128], [110, 96], [75, 60], [40, 30], [12, null]];
    list.textContent = "";
    spans.forEach(function (s) {
      addRow(C.toISO(today - s[0]), s[1] === null ? "" : C.toISO(today - s[1]));
    });
    planStart.value = C.toISO(today + 30);
    planLen.value = "20";
    update();
  }

  function update() {
    var v = validate(readRows());
    render(v);
    save();
  }

  document.getElementById("calc-add").addEventListener("click", function () {
    var li = addRow("", "");
    li.querySelector("input").focus();
  });
  document.getElementById("calc-example").addEventListener("click", example);
  document.getElementById("calc-clear").addEventListener("click", function () {
    list.textContent = "";
    addRow("", "");
    planStart.value = "";
    planLen.value = "";
    update();
  });
  form.addEventListener("input", update);
  form.addEventListener("submit", function (e) { e.preventDefault(); });

  // --- početak ---

  planStart.min = todayISO;
  var saved = load();
  if (saved && saved.stays.length) {
    saved.stays.forEach(function (s) { addRow(s.entry, s.exit); });
    planStart.value = saved.planStart || "";
    planLen.value = saved.planLen || "";
  } else {
    addRow("", "");
  }
  update();
})();
