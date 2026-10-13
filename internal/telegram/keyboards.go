package telegram

const (
	BtnExitedSerbia   = "Izašao iz Srbije"
	BtnReturnedSerbia = "Vratio se u Srbiju"
	BtnMyDays         = "Moji dani"
)

func DriverKeyboard() ReplyKeyboardMarkup {
	return ReplyKeyboardMarkup{
		ResizeKeyboard: true,
		Keyboard: [][]KeyboardButton{
			{{Text: BtnExitedSerbia}, {Text: BtnReturnedSerbia}},
			{{Text: BtnMyDays}},
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
