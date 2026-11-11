package panel

import "strings"

// countryLang is the browser language a local user of each country most
// often has. Anti-detect profiles should match the proxy's location (time
// zone, language, geolocation), or the mismatch itself becomes a signal.
var countryLang = map[string]string{
	"VN": "vi-VN", "US": "en-US", "GB": "en-GB", "CA": "en-CA", "AU": "en-AU", "NZ": "en-NZ", "IE": "en-IE",
	"SG": "en-SG", "IN": "en-IN", "PH": "en-PH", "ZA": "en-ZA", "NG": "en-NG", "KE": "en-KE",
	"MY": "ms-MY", "ID": "id-ID", "TH": "th-TH", "KH": "km-KH", "LA": "lo-LA", "MM": "my-MM",
	"JP": "ja-JP", "KR": "ko-KR", "CN": "zh-CN", "TW": "zh-TW", "HK": "zh-HK",
	"DE": "de-DE", "AT": "de-AT", "CH": "de-CH", "FR": "fr-FR", "BE": "fr-BE", "LU": "fr-LU", "NL": "nl-NL",
	"ES": "es-ES", "MX": "es-MX", "AR": "es-AR", "CO": "es-CO", "CL": "es-CL", "PE": "es-PE",
	"IT": "it-IT", "PT": "pt-PT", "BR": "pt-BR", "PL": "pl-PL", "CZ": "cs-CZ", "SK": "sk-SK",
	"HU": "hu-HU", "RO": "ro-RO", "BG": "bg-BG", "GR": "el-GR", "TR": "tr-TR", "RU": "ru-RU",
	"UA": "uk-UA", "SE": "sv-SE", "NO": "nb-NO", "DK": "da-DK", "FI": "fi-FI", "EE": "et-EE",
	"LV": "lv-LV", "LT": "lt-LT", "IL": "he-IL", "AE": "ar-AE", "SA": "ar-SA", "EG": "ar-EG",
	"PK": "ur-PK", "BD": "bn-BD", "LK": "si-LK",
}

// langFor returns the usual browser language of a country ("" if unknown).
func langFor(country string) string { return countryLang[strings.ToUpper(country)] }

// splitLoc turns ipinfo's "lat,long" into its two parts.
func splitLoc(loc string) (lat, long string) {
	lat, long, _ = strings.Cut(loc, ",")
	return strings.TrimSpace(lat), strings.TrimSpace(long)
}
