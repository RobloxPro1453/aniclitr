package pathx

import (
	"os"
	"strings"
	"testing"
)

// Only the pure helpers: tests never edit the real PATH.
func TestCleanComparesEntries(t *testing.T) {
	t.Setenv("ANICLITR_T", `C:\Tools`)
	same := func(x, y string) bool { return strings.EqualFold(clean(x), clean(y)) }
	if !same(`%ANICLITR_T%\bin\`, `c:\tools\bin`) {
		t.Fatalf("%%VAR%% and trailing separator: %q vs %q", clean(`%ANICLITR_T%\bin\`), clean(`c:\tools\bin`))
	}
	if same(`C:\tools\bin2`, `C:\tools\bin`) {
		t.Fatal("different folders must differ")
	}
	if got := split("a" + string(os.PathListSeparator) + " " + string(os.PathListSeparator) + "b"); len(got) != 2 {
		t.Fatalf("empty entries dropped: %q", got)
	}
}
