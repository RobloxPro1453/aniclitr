package notify

import "testing"

func TestXMLEscapeAndDedup(t *testing.T) {
	got := xmlEscape(`Frieren <S1> & "Bölüm 3" 'Yeni'`)
	want := `Frieren &lt;S1&gt; &amp; &quot;Bölüm 3&quot; &apos;Yeni&apos;`
	if got != want {
		t.Fatalf("xmlEscape mismatch:\ngot:  %s\nwant: %s", got, want)
	}
	// Dedup prevents duplicate notifications for the same key.
	key := "test-ep-dedup-key"
	mu.Lock()
	seen[key] = true
	mu.Unlock()
	if SendOnce(key, "a", "b") {
		t.Fatal("expected duplicate key to be suppressed")
	}
}
