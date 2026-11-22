package ui

// chartRegions are the chart regions offered by the country picker:
// Global first, then by name. Copied from ytm-player (MIT)
// services/regions.py at 6f496bb: YouTube's advertised list plus six
// regions that historically returned charts.
var chartRegions = []struct{ code, name string }{
	{"ZZ", "Global"},
	{"AR", "Argentina"},
	{"AU", "Australia"},
	{"AT", "Austria"},
	{"BE", "Belgium"},
	{"BO", "Bolivia"},
	{"BR", "Brazil"},
	{"CA", "Canada"},
	{"CL", "Chile"},
	{"CO", "Colombia"},
	{"CR", "Costa Rica"},
	{"CZ", "Czechia"},
	{"DK", "Denmark"},
	{"DO", "Dominican Republic"},
	{"EC", "Ecuador"},
	{"EG", "Egypt"},
	{"SV", "El Salvador"},
	{"EE", "Estonia"},
	{"FI", "Finland"},
	{"FR", "France"},
	{"DE", "Germany"},
	{"GT", "Guatemala"},
	{"HN", "Honduras"},
	{"HK", "Hong Kong"},
	{"HU", "Hungary"},
	{"IS", "Iceland"},
	{"IN", "India"},
	{"ID", "Indonesia"},
	{"IE", "Ireland"},
	{"IL", "Israel"},
	{"IT", "Italy"},
	{"JP", "Japan"},
	{"KE", "Kenya"},
	{"LU", "Luxembourg"},
	{"MY", "Malaysia"},
	{"MX", "Mexico"},
	{"NL", "Netherlands"},
	{"NZ", "New Zealand"},
	{"NI", "Nicaragua"},
	{"NG", "Nigeria"},
	{"NO", "Norway"},
	{"PA", "Panama"},
	{"PY", "Paraguay"},
	{"PE", "Peru"},
	{"PL", "Poland"},
	{"PT", "Portugal"},
	{"RO", "Romania"},
	{"RU", "Russia"},
	{"SA", "Saudi Arabia"},
	{"RS", "Serbia"},
	{"SG", "Singapore"},
	{"ZA", "South Africa"},
	{"KR", "South Korea"},
	{"ES", "Spain"},
	{"SE", "Sweden"},
	{"CH", "Switzerland"},
	{"TW", "Taiwan"},
	{"TZ", "Tanzania"},
	{"TH", "Thailand"},
	{"TR", "Turkey"},
	{"UG", "Uganda"},
	{"UA", "Ukraine"},
	{"AE", "United Arab Emirates"},
	{"GB", "United Kingdom"},
	{"US", "United States"},
	{"UY", "Uruguay"},
	{"VN", "Vietnam"},
	{"ZW", "Zimbabwe"},
}

// regionName is the display name of a region code (the code itself when
// unknown).
func regionName(code string) string {
	for _, r := range chartRegions {
		if r.code == code {
			return r.name
		}
	}
	return code
}
