package docsgen

import (
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Commands flattens the command tree below root, depth first in cobra's order, skipping hidden commands and cobra's own help and completion commands.
func Commands(root *cobra.Command) []Command {
	var out []Command

	var visit func(c *cobra.Command)

	visit = func(c *cobra.Command) {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			return
		}

		if c != root {
			out = append(out, Command{
				Path: c.CommandPath(), Use: c.UseLine(), Short: c.Short, Long: paragraphs(c.Long),
				Aliases: c.Aliases, Flags: flags(c.LocalNonPersistentFlags()), Runnable: c.Runnable(),
			})
		}

		for _, sub := range c.Commands() {
			visit(sub)
		}
	}

	visit(root)

	return out
}

// CLIReference renders the CLI reference page.
func CLIReference(root *cobra.Command) ([]byte, error) {
	return render("cli.md.tmpl", map[string]any{
		"Root":     root.Name(),
		"Short":    root.Short,
		"Global":   flags(root.PersistentFlags()),
		"Commands": Commands(root),
	})
}

// usageDefault finds `(default "x")` or `(default x)` in a flag's usage text.
var usageDefault = regexp.MustCompile(`\(default (?:"([^"]*)"|([^\s")]+)\))`)

func flags(fs *pflag.FlagSet) []Flag {
	var out []Flag

	fs.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}

		def := f.DefValue
		if def == "[]" || (f.Value.Type() == "bool" && def == "false") {
			def = ""
		}

		// Encore's flags register no default, so a config value can still win, and spell it out in the usage text instead.
		if m := usageDefault.FindStringSubmatch(f.Usage); def == "" && m != nil {
			def = m[1] + m[2]
		}

		out = append(out, Flag{Name: f.Name, Shorthand: f.Shorthand, Type: f.Value.Type(), Default: def, Usage: f.Usage})
	})

	return out
}

func paragraphs(text string) []string {
	var out []string

	for p := range strings.SplitSeq(strings.TrimSpace(text), "\n\n") {
		if p = strings.Join(strings.Fields(p), " "); p != "" {
			out = append(out, p)
		}
	}

	return out
}
