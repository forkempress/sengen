package telegram

// driverHelpText is the most important instruction in the whole system: the
// count is only as accurate as the driver's button presses, so it explains
// exactly when to press, what to do after forgetting, and why transit
// countries matter.
//
// It talks about the Schengen border, like the buttons, rather than
// "leaving Serbia": on routes through BiH, Montenegro, North Macedonia or Albania the
// two are not the same, and days spent there do not count.
const driverHelpText = `Kako se koristi:

` + BtnEnteredSchengen + ` — pritisni kad uđeš u Šengen.
` + BtnExitedSchengen + ` — pritisni kad izađeš iz Šengena.
` + BtnMyDays + ` — koliko dana si iskoristio i do kada smeš da ostaneš.

Važno:
• Broji se granica Šengena, ne Srbije. Ako ideš preko BiH, Crne Gore, Makedonije ili Albanije, dani tamo se ne broje — pritisni tek kad uđeš u Šengen (npr. u Hrvatsku ili Grčku).
• Dan ulaska i dan izlaska se broje kao celi dani.
• Računaju se i privatna putovanja u Šengen — koristi ista dugmad.
• Dugme upisuje današnji datum. Između 23:00 i 01:30 bot pita koji datum je na pečatu (na granici sa Bugarskom, Rumunijom i Grčkom tamo je sat više).
• Ako si zaboravio da pritisneš na granici, pritisni čim se setiš i javi dispečeru tačan datum da ga ispravi.
• Ako pritisneš „` + BtnExitedSchengen + `“ a ulazak nije evidentiran, bot će pitati kog datuma si ušao u Šengen.
• Pogrešno pritisnuto? Dugme „Poništi" ispod potvrde važi 10 minuta.
• Ako dugo ne javiš izlazak, bot će te pitati da li si još u Šengenu.

Evidencija služi za internu kontrolu. Zvanično stanje vodi EES sistem na granici.`

// adminHelpText is shared by the welcome message after linking and /pomoc,
// so the two can never list different commands.
const adminHelpText = `Komande:
/pregled — semafor svih vozača, sortiran po riziku
/vozac <ime> — dani, do kada sme da ostane, sledeće oslobađanje
/planer <ime> <dd.mm>-<dd.mm> — može li na turu (ili <dd.mm> <broj dana>)
/planer <ime> — sačuvani planovi vozača
/dodaj <ime> — dodaj vozača
/link <ime> — link za povezivanje vozača sa botom
/ispravi — poslednjih 5 izmena, sa poništavanjem
/sablon — format za uvoz
/pomoc — ova poruka

Brzi unos: pošaljite tekst poput
Marko 22.04-29.04, 06.05-15.05, 08.07-14.07 p
(p = privatno). Bot pokaže tumačenje i traži potvrdu.

Uvoz: pošaljite .csv fajl (vozač, ulaz, izlaz, tip). Ništa se ne upisuje pre potvrde.`

// isHelpRequest matches the ways someone may ask for help: the keyboard
// button, or the command typed with or without the diacritic.
func isHelpRequest(text string) bool {
	switch text {
	case BtnHelp, "/pomoc", "/pomoć", "/help":
		return true
	}
	return false
}
