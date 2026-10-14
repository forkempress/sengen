package telegram

// The buttons are named after the Schengen border, not Serbia's: on routes
// through BiH, Montenegro, North Macedonia or Albania leaving Serbia is not
// entering Schengen.
const (
	BtnEnteredSchengen = "▶ Ušao u Šengen"
	BtnExitedSchengen  = "◀ Izašao iz Šengena"
	BtnMyDays          = "Moji dani"
	BtnHelp            = "Pomoć"
)

// Telegram keeps the keyboard a driver got last, and a button press arrives
// as its text. Drivers linked before the rename still have these buttons
// until a reply replaces their keyboard, so the old texts keep working.
const (
	legacyBtnExitedSerbia   = "Izašao iz Srbije"
	legacyBtnReturnedSerbia = "Vratio se u Srbiju"
)

// driverButton maps what a driver sent to the button it stands for, with
// the old names as aliases. legacy reports an old name: that driver still
// has the old keyboard.
func driverButton(text string) (btn string, legacy bool) {
	switch text {
	case legacyBtnExitedSerbia:
		return BtnEnteredSchengen, true
	case legacyBtnReturnedSerbia:
		return BtnExitedSchengen, true
	}
	return text, false
}

// isDriverButton reports whether btn is one of the keyboard's buttons.
func isDriverButton(btn string) bool {
	switch btn {
	case BtnEnteredSchengen, BtnExitedSchengen, BtnMyDays, BtnHelp:
		return true
	}
	return false
}

func DriverKeyboard() ReplyKeyboardMarkup {
	return ReplyKeyboardMarkup{
		ResizeKeyboard: true,
		Keyboard: [][]KeyboardButton{
			{{Text: BtnEnteredSchengen}, {Text: BtnExitedSchengen}},
			{{Text: BtnMyDays}, {Text: BtnHelp}},
		},
	}
}

func InlineButton(text, callbackData string) InlineKeyboardButton {
	return InlineKeyboardButton{Text: text, CallbackData: callbackData}
}

func InlineRow(buttons ...InlineKeyboardButton) []InlineKeyboardButton { return buttons }

func InlineKeyboard(rows ...[]InlineKeyboardButton) InlineKeyboardMarkup {
	return InlineKeyboardMarkup{InlineKeyboard: rows}
}
