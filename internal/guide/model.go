package guide

// Page is one HTML file in the offline guide.
type Page struct {
	ID       string
	File     string
	Nav      string
	Title    string
	Kicker   string
	Lead     string
	Sections []Section
	Commands []Command
}

// Section is a headed block on a page.
type Section struct {
	ID         string
	Title      string
	Paragraphs []string
	Callout    string
	Bullets    []string
	Codes      []Code
	Table      *Table
	Links      []Link
}

// Code is a copyable example.
type Code struct {
	Title string
	Body  string
	Note  string
}

// Table is a small comparison or matrix.
type Table struct {
	Caption string
	Headers []string
	Rows    [][]string
}

// Link is a card pointing at another guide page.
type Link struct {
	Href  string
	Title string
	Text  string
}

// Command is one CLI entry on the help page.
type Command struct {
	Name       string
	Summary    string
	Paragraphs []string
	Flags      []string
	Examples   []Code
}

// CommandNames returns the command paths documented on the help page.
func CommandNames() []string {
	cmds := Commands()
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.Name
	}
	return out
}
