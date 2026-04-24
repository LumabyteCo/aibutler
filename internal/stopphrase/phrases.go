package stopphrase

// defaultPhrases maps language codes to stop/cancel phrases.
var defaultPhrases = map[string][]string{
	"en": {"stop", "cancel", "quit", "exit", "abort", "enough", "nevermind", "never mind"},
	"fr": {"arrête", "arrêter", "stop", "annuler", "quitter"},
	"es": {"para", "parar", "detener", "cancelar", "basta"},
	"pt": {"pare", "parar", "cancelar", "chega"},
	"de": {"stopp", "stop", "aufhören", "abbrechen"},
	"it": {"ferma", "fermati", "stop", "basta", "annulla"},
	"nl": {"stop", "stoppen", "annuleren"},
	"ru": {"стоп", "остановись", "хватит", "отмена"},
	"ja": {"やめて", "ストップ", "止めて", "中止"},
	"ko": {"그만", "멈춰", "중지", "취소"},
	"zh": {"停", "停止", "取消", "算了"},
	"ar": {"توقف", "قف", "أوقف", "إلغاء"},
	"hi": {"रुको", "बंद करो", "रोको"},
	"tr": {"dur", "durdur", "iptal", "vazgeç"},
}

// cancelPhrases are a subset that indicate cancel (stronger than stop).
var cancelPhrases = map[string][]string{
	"en": {"cancel", "abort"},
	"fr": {"annuler"},
	"es": {"cancelar"},
	"pt": {"cancelar"},
	"de": {"abbrechen"},
	"it": {"annulla"},
	"nl": {"annuleren"},
	"ru": {"отмена"},
	"ja": {"中止"},
	"ko": {"취소"},
	"zh": {"取消"},
	"ar": {"إلغاء"},
	"hi": {},
	"tr": {"iptal"},
}
