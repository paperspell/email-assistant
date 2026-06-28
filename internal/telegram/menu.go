package telegram

import "github.com/PaulSonOfLars/gotgbot/v2"

// btn is a small constructor for an inline keyboard button.
func btn(text, data string) gotgbot.InlineKeyboardButton {
	return gotgbot.InlineKeyboardButton{Text: text, CallbackData: data}
}

// actionKeyboard is the default per-notification keyboard.
func actionKeyboard(emailID string) gotgbot.InlineKeyboardMarkup {
	return gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
			btn("✓ Handled", "handled:"+emailID),
			btn("✗ Ignore", "ignore:"+emailID),
			btn("ℹ Details", "details:"+emailID),
		}},
	}
}

// ignoreMenu is shown when the user taps Ignore: it lets them turn this ignore
// into a reusable per-account rule, or ignore just this once.
func ignoreMenu(emailID string, hasListID bool) gotgbot.InlineKeyboardMarkup {
	rows := [][]gotgbot.InlineKeyboardButton{
		{btn("This sender", "ign_sender:"+emailID), btn("This domain", "ign_domain:"+emailID)},
	}
	if hasListID {
		rows = append(rows, []gotgbot.InlineKeyboardButton{btn("This mailing list", "ign_listid:"+emailID)})
	}
	rows = append(rows,
		[]gotgbot.InlineKeyboardButton{btn("Subject like this", "ign_subject:"+emailID)},
		[]gotgbot.InlineKeyboardButton{btn("Describe a reason", "ign_reason:"+emailID)},
		[]gotgbot.InlineKeyboardButton{btn("Just this once", "ign_once:"+emailID), btn("Cancel", "ign_cancel:"+emailID)},
	)
	return gotgbot.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// subjectConfirmMenu confirms a suggested subject pattern.
func subjectConfirmMenu(emailID string) gotgbot.InlineKeyboardMarkup {
	return gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
			btn("Use it", "subj_use:"+emailID),
			btn("Edit", "subj_edit:"+emailID),
			btn("Cancel", "subj_cancel:"+emailID),
		}},
	}
}

// promoteRuleMenu is offered when a promoted email was hidden by an explicit rule.
func promoteRuleMenu(ruleID, emailID string) gotgbot.InlineKeyboardMarkup {
	return gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
			btn("Remove rule", "prom_rmrule:"+ruleID),
			btn("Add exception", "prom_allow:"+emailID),
			btn("Keep rule", "prom_keep:"+emailID),
		}},
	}
}

// promoteAllowMenu is offered when no explicit rule hid the promoted email.
func promoteAllowMenu(emailID string) gotgbot.InlineKeyboardMarkup {
	return gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
			btn("Yes, always important", "prom_allow:"+emailID),
			btn("No, just this time", "prom_no:"+emailID),
		}},
	}
}
