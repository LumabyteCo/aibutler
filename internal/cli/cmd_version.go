package cli

import (
	"fmt"
	"io"
)

// CmdVersion prints the application version.
func CmdVersion(w io.Writer) {
	fmt.Fprintf(w, "aibutler v%s\n", Version)
}
