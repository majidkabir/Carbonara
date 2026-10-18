package explain

import "strings"

// Markdown renders the sheet as Markdown, as the web page's download does:
// a heading per group, each step with its text, its arithmetic in a code
// block and its basis as a list, then the note and the references.
func Markdown(sh Sheet) string {
	id := sh.ShipmentID
	if id == "" {
		id = "shipment"
	}
	var b strings.Builder
	line := func(s ...string) {
		for _, x := range s {
			b.WriteString(x)
			b.WriteByte('\n')
		}
	}
	line("# Calculation: "+id, "", sh.Summary, "")
	group := ""
	for _, st := range sh.Steps {
		if st.Group != group {
			line("## "+st.Group, "")
			group = st.Group
		}
		line("### "+st.Title, "")
		if st.Text != "" {
			line(st.Text, "")
		}
		if st.Formula != "" {
			line("```", st.Formula, "```", "")
		}
		for _, x := range st.Basis {
			line("- " + x)
		}
		if len(st.Basis) > 0 {
			line("")
		}
	}
	line("---", "", sh.Note, "", "References:", "")
	for _, r := range sh.References {
		line("- " + r)
	}
	return b.String()
}
