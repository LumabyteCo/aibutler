package shell

import (
	"fmt"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Parse parses a command string into an AST.
func Parse(command string) (*syntax.File, error) {
	parser := syntax.NewParser(syntax.Variant(syntax.LangPOSIX))
	prog, err := parser.Parse(strings.NewReader(command), "command")
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return prog, nil
}

// Validate checks the AST for dangerous features.
// Rejects: compound commands (;, &&, ||, |), command substitution, parameter
// expansion, globbing, and background execution.
func Validate(prog *syntax.File) error {
	// Reject multiple statements (semicolons).
	if len(prog.Stmts) > 1 {
		return fmt.Errorf("multiple statements (;) not allowed")
	}
	if len(prog.Stmts) == 0 {
		return fmt.Errorf("empty command")
	}

	var validationErr error
	syntax.Walk(prog, func(node syntax.Node) bool {
		if validationErr != nil {
			return false
		}

		switch n := node.(type) {
		case *syntax.BinaryCmd:
			validationErr = fmt.Errorf("compound operator %q not allowed", n.Op.String())
			return false

		case *syntax.Stmt:
			if n.Background {
				validationErr = fmt.Errorf("background operator (&) not allowed")
				return false
			}

		case *syntax.CmdSubst:
			validationErr = fmt.Errorf("command substitution not allowed")
			return false

		case *syntax.ParamExp:
			validationErr = fmt.Errorf("parameter expansion $%s not allowed", n.Param.Value)
			return false

		case *syntax.ProcSubst:
			validationErr = fmt.Errorf("process substitution not allowed")
			return false

		case *syntax.Word:
			for _, part := range n.Parts {
				if lit, ok := part.(*syntax.Lit); ok {
					if containsGlob(lit.Value) {
						validationErr = fmt.Errorf("glob pattern %q not allowed", lit.Value)
						return false
					}
				}
			}
		}

		return true
	})

	return validationErr
}

// ExtractCommandName returns the base command name from the parsed program.
func ExtractCommandName(prog *syntax.File) string {
	if len(prog.Stmts) == 0 {
		return ""
	}
	call, ok := prog.Stmts[0].Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 {
		return ""
	}
	return wordToString(call.Args[0])
}

func containsGlob(s string) bool {
	return strings.ContainsAny(s, "*?[")
}

func wordToString(w *syntax.Word) string {
	var sb strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			sb.WriteString(p.Value)
		case *syntax.SglQuoted:
			sb.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, inner := range p.Parts {
				if lit, ok := inner.(*syntax.Lit); ok {
					sb.WriteString(lit.Value)
				}
			}
		}
	}
	return sb.String()
}
