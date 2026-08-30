package telegram

import (
	"github.com/PaulSonOfLars/gotgbot/v2"

	"github.com/paperspell/email-assistant/internal/i18n"
)

// btn is a small constructor for an inline keyboard button.
func btn(text, data string) gotgbot.InlineKeyboardButton {
	return gotgbot.InlineKeyboardButton{Text: text, CallbackData: data}
}

// actionKeyboard is the default per-notification keyboard.
func actionKeyboard(p *i18n.Printer, emailID string) gotgbot.InlineKeyboardMarkup {
	return gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
			btn(p.T("btn_handled"), "handled:"+emailID),
			btn(p.T("btn_ignore"), "ignore:"+emailID),
			btn(p.T("btn_details"), "details:"+emailID),
		}},
	}
}

// ignoreMenu is shown when the user taps Ignore: it lets them turn this ignore
// into a reusable per-account rule, or ignore just this once.
func ignoreMenu(p *i18n.Printer, emailID string, hasListID bool) gotgbot.InlineKeyboardMarkup {
	rows := [][]gotgbot.InlineKeyboardButton{
		{btn(p.T("btn_ign_sender"), "ign_sender:"+emailID), btn(p.T("btn_ign_domain"), "ign_domain:"+emailID)},
	}
	if hasListID {
		rows = append(rows, []gotgbot.InlineKeyboardButton{btn(p.T("btn_ign_listid"), "ign_listid:"+emailID)})
	}
	rows = append(rows,
		[]gotgbot.InlineKeyboardButton{btn(p.T("btn_ign_subject"), "ign_subject:"+emailID)},
		[]gotgbot.InlineKeyboardButton{btn(p.T("btn_ign_reason"), "ign_reason:"+emailID)},
		[]gotgbot.InlineKeyboardButton{
			btn(p.T("btn_ign_once"), "ign_once:"+emailID),
			btn(p.T("btn_cancel"), "ign_cancel:"+emailID),
		},
	)
	return gotgbot.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// subjectConfirmMenu confirms a suggested subject pattern.
func subjectConfirmMenu(p *i18n.Printer, emailID string) gotgbot.InlineKeyboardMarkup {
	return gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
			btn(p.T("btn_subj_use"), "subj_use:"+emailID),
			btn(p.T("btn_subj_edit"), "subj_edit:"+emailID),
			btn(p.T("btn_cancel"), "subj_cancel:"+emailID),
		}},
	}
}

// promoteRuleMenu is offered when a promoted email was hidden by an explicit rule.
func promoteRuleMenu(p *i18n.Printer, ruleID, emailID string) gotgbot.InlineKeyboardMarkup {
	return gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
			btn(p.T("btn_prom_rmrule"), "prom_rmrule:"+ruleID),
			btn(p.T("btn_prom_allow"), "prom_allow:"+emailID),
			btn(p.T("btn_prom_keep"), "prom_keep:"+emailID),
		}},
	}
}

// promoteAllowMenu is offered when no explicit rule hid the promoted email.
func promoteAllowMenu(p *i18n.Printer, emailID string) gotgbot.InlineKeyboardMarkup {
	return gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
			btn(p.T("btn_prom_yes"), "prom_allow:"+emailID),
			btn(p.T("btn_prom_no"), "prom_no:"+emailID),
		}},
	}
}
