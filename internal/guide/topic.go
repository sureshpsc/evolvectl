package guide

// FileForTopic maps a docs topic onto a page in the guide.
// An empty topic is the start page.
func FileForTopic(topic string) (string, bool) {
	switch topic {
	case "", "index", "start", "quickstart", "list":
		return "index.html", true
	case "help", "commands":
		return "help.html", true
	case "copybara", "providers", "session":
		return "copybara.html", true
	case "languages", "adapters":
		return "languages.html", true
	case "how-it-works", "campaign", "ui":
		return "how-it-works.html", true
	case "recipes":
		return "recipes.html", true
	case "examples":
		return "examples.html", true
	case "troubleshooting":
		return "troubleshooting.html", true
	default:
		return "", false
	}
}

// Topics lists the page names accepted by FileForTopic, in nav order.
func Topics() []string {
	return []string{"index", "copybara", "languages", "how-it-works", "help", "recipes", "examples", "troubleshooting"}
}
