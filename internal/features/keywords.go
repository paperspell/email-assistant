package features

import (
	"unicode"
	"unicode/utf8"
)

// Each slice contains keywords for one semantic group across all supported languages:
// English, Polish, Russian, Romanian, Italian, Belarusian, Ukrainian,
// Spanish, Portuguese, French, German, Kazakh, Hebrew.

var keywordsUrgent = []string{
	// EN
	"urgent", "asap", "immediately", "critical", "emergency",
	// PL
	"pilny", "pilnie", "natychmiast", "niezwłocznie",
	// RU
	"срочно", "немедленно", "критический", "экстренно",
	// RO
	"urgent", "imediat", "critic",
	// IT
	"urgente", "immediatamente", "critico",
	// BE
	"тэрміновы", "неадкладна",
	// UK
	"терміново", "негайно", "критично",
	// ES
	"urgente", "inmediatamente", "crítico",
	// PT
	"urgente", "imediatamente", "crítico",
	// FR
	"urgent", "immédiatement", "critique",
	// DE
	"dringend", "sofort", "kritisch",
	// KK
	"шұғыл", "дереу",
	// HE
	"דחוף", "מיידי", "קריטי",
}

var keywordsMeeting = []string{
	// EN
	"meeting", "call", "appointment", "conference", "schedule", "invite", "invitation",
	// PL
	"spotkanie", "zebranie", "konferencja", "wizyta", "zaproszenie",
	// RU
	"встреча", "совещание", "конференция", "созвон", "приглашение",
	// RO
	"întâlnire", "ședință", "conferință", "invitație",
	// IT
	"riunione", "incontro", "conferenza", "invito",
	// BE
	"сустрэча", "нарада", "канферэнцыя", "запрашэнне",
	// UK
	"зустріч", "нарада", "конференція", "запрошення",
	// ES
	"reunión", "cita", "conferencia", "junta", "invitación",
	// PT
	"reunião", "encontro", "conferência", "convite",
	// FR
	"réunion", "rendez-vous", "conférence", "invitation",
	// DE
	"treffen", "meeting", "konferenz", "termin", "einladung",
	// KK
	"кездесу", "жиын", "шақыру",
	// HE
	"פגישה", "שיחה", "תור", "הזמנה",
}

// keywordsMoney marks mail about money actually moving: a payment taken, an
// invoice due, a receipt, a refund, a failed charge. It drives a threshold
// bypass, so it is matched on whole words only and deliberately excludes short
// or ambiguous stems ("pay", "due", "чек") that would fire on unrelated text.
var keywordsMoney = []string{
	// EN
	"invoice", "payment", "receipt", "charged", "refund", "billing",
	"overdue", "transaction", "debited", "subscription renewal",
	// PL
	"faktura", "płatność", "płatności", "rachunek", "zapłata", "obciążenie",
	"opłata", "opłacie", "przelew",
	// RU
	"счёт", "счет", "оплата", "оплате", "платёж", "платеж", "квитанция",
	"списание", "начисление", "задолженность",
	// UK
	"рахунок", "оплата", "платіж", "квитанція",
	// BE
	"рахунак", "аплата", "плацёж",
	// DE
	"rechnung", "zahlung", "beleg", "abbuchung",
	// ES
	"factura", "pago", "recibo", "cargo",
	// FR
	"facture", "paiement", "reçu", "prélèvement",
	// IT
	"fattura", "pagamento", "ricevuta", "addebito",
	// PT
	"fatura", "pagamento", "recibo", "cobrança",
}

var keywordsInvoice = []string{
	// EN
	"invoice", "payment", "bill", "receipt", "due", "overdue", "pay",
	"charged", "refund", "billing", "transaction", "debited",
	// PL
	"faktura", "płatność", "rachunek", "zapłata", "przelew", "zaległy",
	"obciążenie", "opłata", "zwrot środków",
	// RU
	"счет", "счёт", "оплата", "платеж", "платёж", "квитанция", "задолженность",
	"списание", "начисление", "чек", "возврат средств",
	// RO
	"factură", "plată", "chitanță", "scadent",
	// IT
	"fattura", "pagamento", "ricevuta", "scadenza",
	// BE
	"рахунак", "аплата", "плацёж",
	// UK
	"рахунок", "оплата", "платіж", "квитанція",
	// ES
	"factura", "pago", "recibo", "vencimiento",
	// PT
	"fatura", "pagamento", "recibo", "vencimento",
	// FR
	"facture", "paiement", "reçu", "échéance",
	// DE
	"rechnung", "zahlung", "quittung", "fälligkeit",
	// KK
	"шот", "төлем",
	// HE
	"חשבונית", "תשלום", "קבלה", "פרעון",
}

var keywordsSecurity = []string{
	// EN
	"security", "password", "login", "verification", "confirm", "suspicious", "breach", "access",
	// PL
	"hasło", "bezpieczeństwo", "weryfikacja", "logowanie", "konto", "podejrzany",
	// RU
	"пароль", "безопасность", "вход", "верификация", "подтверждение", "подозрительный",
	// RO
	"parolă", "securitate", "verificare", "autentificare", "cont",
	// IT
	"password", "sicurezza", "verifica", "accesso", "account",
	// BE
	"пароль", "бяспека", "верыфікацыя", "уваход",
	// UK
	"пароль", "безпека", "верифікація", "підтвердження", "вхід",
	// ES
	"contraseña", "seguridad", "verificación", "acceso", "cuenta",
	// PT
	"senha", "segurança", "verificação", "acesso", "conta",
	// FR
	"mot de passe", "sécurité", "vérification", "connexion", "compte",
	// DE
	"passwort", "sicherheit", "verifizierung", "zugang", "konto",
	// KK
	"құпия сөз", "қауіпсіздік", "тіркелгі",
	// HE
	"סיסמה", "אבטחה", "אימות", "כניסה", "חשבון",
}

var keywordsDeadline = []string{
	// EN
	"deadline", "due date", "expires", "expiration", "reminder", "last day", "final",
	// PL
	"termin", "deadline", "wygasa", "przypomnienie", "ostatni dzień",
	// RU
	"дедлайн", "срок", "истекает", "напоминание", "последний день",
	// RO
	"termen", "scadență", "expiră", "reminder", "ultima zi",
	// IT
	"scadenza", "termine", "promemoria", "ultimo giorno",
	// BE
	"тэрмін", "нагадванне", "дэдлайн", "апошні дзень",
	// UK
	"дедлайн", "термін", "нагадування", "останній день",
	// ES
	"fecha límite", "plazo", "vence", "recordatorio", "último día",
	// PT
	"prazo", "vencimento", "lembrete", "último dia",
	// FR
	"délai", "échéance", "rappel", "date limite", "dernier jour",
	// DE
	"frist", "deadline", "ablauf", "erinnerung", "letzter tag",
	// KK
	"мерзім", "дедлайн", "еске салу",
	// HE
	"דדליין", "מועד אחרון", "תזכורת", "יום אחרון",
}

var keywordsInterview = []string{
	// EN
	"interview", "job offer", "position", "candidate", "offer letter", "recruitment", "vacancy",
	// PL
	"rozmowa kwalifikacyjna", "oferta pracy", "rekrutacja", "stanowisko", "kandydat",
	// RU
	"собеседование", "вакансия", "предложение работы", "рекрутинг", "кандидат",
	// RO
	"interviu", "ofertă de muncă", "recrutare", "candidat", "post",
	// IT
	"colloquio", "offerta di lavoro", "posizione", "candidato", "assunzione",
	// BE
	"сумоўе", "вакансія", "прапанова працы", "кандыдат",
	// UK
	"співбесіда", "вакансія", "пропозиція роботи", "кандидат",
	// ES
	"entrevista", "oferta de trabajo", "vacante", "candidato", "reclutamiento",
	// PT
	"entrevista", "oferta de emprego", "vaga", "candidato", "recrutamento",
	// FR
	"entretien", "offre d'emploi", "poste", "candidat", "recrutement",
	// DE
	"vorstellungsgespräch", "stellenangebot", "stelle", "bewerber", "bewerbung",
	// KK
	"сұхбат", "жұмыс ұсынысы", "бос орын", "үміткер",
	// HE
	"ראיון", "הצעת עבודה", "משרה", "מועמד", "גיוס",
}

var keywordsGovernment = []string{
	// EN
	"government", "official", "authority", "ministry", "department", "court", "tax", "compliance", "regulation",
	// PL
	"urząd", "rząd", "ministerstwo", "sąd", "podatek", "compliance", "organ",
	// RU
	"правительство", "министерство", "суд", "налог", "официальный", "ведомство",
	// RO
	"guvern", "ministerul", "tribunal", "impozit", "autoritate", "instituție",
	// IT
	"governo", "ministero", "tribunale", "imposta", "autorità", "ente",
	// BE
	"урад", "міністэрства", "суд", "падатак", "установа",
	// UK
	"уряд", "міністерство", "суд", "податок", "установа",
	// ES
	"gobierno", "ministerio", "tribunal", "impuesto", "autoridad", "organismo",
	// PT
	"governo", "ministério", "tribunal", "imposto", "autoridade", "organismo",
	// FR
	"gouvernement", "ministère", "tribunal", "impôt", "autorité", "organisme",
	// DE
	"regierung", "ministerium", "gericht", "steuer", "behörde", "amt",
	// KK
	"үкімет", "министрлік", "сот", "салық", "мекеме",
	// HE
	"ממשלה", "משרד", "בית משפט", "מס", "רשות",
}

var keywordsSchool = []string{
	// EN
	"school", "university", "college", "student", "grade", "homework", "parent", "teacher", "education", "class", "exam",
	// PL
	"szkoła", "uczelnia", "uczeń", "ocena", "rodzic", "nauczyciel", "lekcja", "egzamin",
	// RU
	"школа", "университет", "студент", "оценка", "родитель", "учитель", "урок", "экзамен",
	// RO
	"școală", "universitate", "elev", "notă", "profesor", "lecție", "examen",
	// IT
	"scuola", "università", "studente", "voto", "genitore", "insegnante", "lezione", "esame",
	// BE
	"школа", "універсітэт", "вучань", "адзнака", "бацька", "настаўнік", "урок", "іспыт",
	// UK
	"школа", "університет", "учень", "оцінка", "батьки", "вчитель", "урок", "іспит",
	// ES
	"escuela", "universidad", "estudiante", "calificación", "profesor", "clase", "examen",
	// PT
	"escola", "universidade", "estudante", "nota", "professor", "aula", "exame",
	// FR
	"école", "université", "étudiant", "note", "professeur", "classe", "examen",
	// DE
	"schule", "universität", "student", "note", "lehrer", "eltern", "unterricht", "prüfung",
	// KK
	"мектеп", "университет", "оқушы", "баға", "мұғалім", "сабақ", "емтихан",
	// HE
	"בית ספר", "אוניברסיטה", "תלמיד", "ציון", "מורה", "הורה", "שיעור", "בחינה",
}

// containsAny reports whether text contains any of the given keywords (case-insensitive).
// containsAnyWord reports whether text contains any keyword as a whole word.
// Unlike containsAny it will not match inside a longer word, so "charged" does
// not fire on "discharged" and "чек" would not fire on "чек-лист". Boundaries
// are unicode-aware: anything that is not a letter or digit separates words.
func containsAnyWord(text string, keywords []string) bool {
	lower := toLower(text)
	for _, kw := range keywords {
		if hasWord(lower, toLower(kw)) {
			return true
		}
	}
	return false
}

func hasWord(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] != needle {
			continue
		}
		if !isWordByte(haystack, i-1) && !isWordByte(haystack, i+len(needle)) {
			return true
		}
	}
	return false
}

// isWordByte reports whether the rune starting at or covering index i is a
// letter or digit. Indexes outside the string count as separators.
func isWordByte(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	// Walk back to the start of the rune so multi-byte letters are judged whole.
	start := i
	for start > 0 && !utf8.RuneStart(s[start]) {
		start--
	}
	r, _ := utf8.DecodeRuneInString(s[start:])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func containsAny(text string, keywords []string) bool {
	lower := toLower(text)
	for _, kw := range keywords {
		if contains(lower, toLower(kw)) {
			return true
		}
	}
	return false
}
