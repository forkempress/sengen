// Package legal holds what the legal pages, the acceptance step and the
// bot's notices share: the provider's details and the current version of
// the terms. Everything still marked [POPUNITI] must be filled in before
// the service is sold.
package legal

// Provider: who runs dani90.rs.
const (
	ProviderName    = "[POPUNITI: ime i prezime ili naziv preduzetnika]"
	ProviderForm    = "[POPUNITI: preduzetnik / fizičko lice / d.o.o.]"
	ProviderAddress = "[POPUNITI: adresa]"
	ProviderIDs     = "[POPUNITI: PIB i MB, ili „nije registrovano“]"
	PrivacyEmail    = "[POPUNITI: npr. privatnost@dani90.rs]"
)

// TermsVersion identifies the terms of use and the data processing
// agreement as they are now. Change it with any material change to either:
// every firm's owner is then asked to accept again at the next sign-in.
const TermsVersion = "2026-10-15"

// LastUpdated is shown on the three legal pages.
const LastUpdated = "15.10.2026."

// Disclaimer is the short notice that the calculation is not legal advice,
// shown under results, in the footer and (DisclaimerBot) in the bot.
const (
	Disclaimer    = "Dani90 je pomoć za planiranje i računa samo iz unetih datuma. Nije povezan sa EU ni sa državnim organima; o ulasku i boravku odlučuje granična policija, a zvanično stanje proverite u EU kalkulatoru kratkog boravka i informacijama o EES-u."
	DisclaimerBot = "ⓘ Procena iz unetih datuma, nije pravni savet. Odluku donosi granična policija; zvanično stanje: EU kalkulator i EES."
)

// ReportNote is the notice on the printable driver report, in Serbian and
// English. It names no app: the report reads as the driver's own record.
const (
	ReportNote   = "Lična evidencija boravaka, nije zvaničan dokument. Dani su izračunati po pravilu 90/180 iz upisanih datuma ulaska i izlaska. O ulasku i boravku odlučuju granični organi."
	ReportNoteEN = "Personal record of stays, not an official document. Days are calculated under the 90/180-day rule from the entry and exit dates recorded. Entry and stay are decided by the border authorities."
)

// Official sources the disclaimer points to.
const (
	EUCalculatorURL = "https://home-affairs.ec.europa.eu/policies/schengen/border-crossing/short-stay-calculator_en"
	EESInfoURL      = "https://travel-europe.europa.eu/en/ees"
)

// PublicBaseURL is where the app's public pages live, for links sent by the
// bot.
const PublicBaseURL = "https://app.dani90.rs"

// Info gathers the constants for templates.
type Info struct {
	ProviderName, ProviderForm, ProviderAddress, ProviderIDs, PrivacyEmail string
	TermsVersion, LastUpdated                                              string
	Disclaimer, EUCalculatorURL, EESInfoURL                                string
	ReportNote, ReportNoteEN                                               string
}

func Get() Info {
	return Info{
		ProviderName: ProviderName, ProviderForm: ProviderForm, ProviderAddress: ProviderAddress,
		ProviderIDs: ProviderIDs, PrivacyEmail: PrivacyEmail,
		TermsVersion: TermsVersion, LastUpdated: LastUpdated,
		Disclaimer: Disclaimer, EUCalculatorURL: EUCalculatorURL, EESInfoURL: EESInfoURL,
		ReportNote: ReportNote, ReportNoteEN: ReportNoteEN,
	}
}
