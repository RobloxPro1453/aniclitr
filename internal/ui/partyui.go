package ui

import (
	"fmt"
	"os/user"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/nyaa"
	"aniclitr/internal/watchparty"
)

// partyScreen manages zero-dependency LAN/TCP Watch Together rooms cleanly
// without adding permanent clutter to the Home or Playing screens.
type partyScreen struct {
	joining bool
	addrIn  input
	nickIn  input
	focus   int // 0 addr, 1 nick
}

func defaultNick() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		n := u.Username
		if i := strings.LastIndexAny(n, `\/`); i >= 0 {
			n = n[i+1:]
		}
		if n != "" {
			return n
		}
	}
	return "İzleyici"
}

func newPartyScreen() *partyScreen {
	p := &partyScreen{
		addrIn: input{placeholder: "ör. 192.168.1.5:47620"},
		nickIn: input{placeholder: "takma ad (isteğe bağlı)"},
	}
	p.nickIn.Set(defaultNick())
	return p
}

func (p *partyScreen) crumb() string { return "birlikte izle" }

func (p *partyScreen) typing() bool { return p.joining }

func (p *partyScreen) hints() []hint {
	if p.joining {
		return []hint{{"enter", "bağlan"}, {"tab", "alan değiştir"}, {"esc", "vazgeç"}}
	}
	return []hint{{"h", "oda kur"}, {"j", "odaya katıl"}, {"c", "adresi kopyala"}, {"x", "odadan çık"}, {"esc", "geri"}}
}

func (p *partyScreen) update(a *App, msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	if p.joining {
		switch k.String() {
		case "esc":
			p.joining = false
			return nil
		case "tab", "shift+tab", "down", "up":
			p.focus = 1 - p.focus
			return nil
		case "enter":
			addr := strings.TrimSpace(p.addrIn.Value())
			nick := strings.TrimSpace(p.nickIn.Value())
			room, err := watchparty.Join(addr, nick)
			if err != nil {
				a.notify(err.Error(), "err")
				return a.tick()
			}
			if a.party != nil {
				a.party.Close()
			}
			a.party = room
			p.joining = false
			a.notify("◈ odaya bağlanıldı: "+room.Addr(), "ok")
			return tea.Batch(a.waitParty(), a.tick())
		default:
			if p.focus == 0 {
				p.addrIn.handle(k)
			} else {
				p.nickIn.handle(k)
			}
			return nil
		}
	}
	switch k.String() {
	case "esc", "q":
		a.pop()
		return nil
	case "h", "H":
		if a.party != nil {
			a.party.Close()
		}
		room, err := watchparty.Host(":47620", defaultNick(), "")
		if err != nil {
			a.notify(err.Error(), "err")
			return a.tick()
		}
		a.party = room
		_ = nyaa.CopyClipboard(room.Addr())
		a.notify("◈ oda açıldı ve adres kopyalandı: "+room.Addr(), "ok")
		return tea.Batch(a.waitParty(), a.tick())
	case "j", "J":
		p.joining = true
		p.focus = 0
		return a.tick()
	case "c", "C":
		if a.party != nil {
			if err := nyaa.CopyClipboard(a.party.Addr()); err != nil {
				a.notify("kopyalanamadı: "+err.Error(), "warn")
			} else {
				a.notify("oda adresi kopyalandı: "+a.party.Addr(), "ok")
			}
			return a.tick()
		}
	case "x", "X":
		if a.party != nil {
			a.party.Close()
			a.party = nil
			a.notify("birlikte izleme odası kapatıldı", "ok")
			return a.tick()
		}
	}
	return nil
}

func (p *partyScreen) view(a *App, w, h int) string {
	cw := clamp(w-8, 52, 82)
	iw := cw - 4
	var body []string

	if a.party != nil {
		role := "MİSAFİR"
		col := cAccent2
		if a.party.IsHost() {
			role, col = "EV SAHİBİ", cOK
		}
		body = append(body,
			center(pill("◈ AKTİF ODA · "+role, col), iw),
			"",
			row2("  "+stMuted.Render("Oda adresi"), fg(cAccent).Bold(true).Render(a.party.Addr()+"  "), iw),
			row2("  "+stMuted.Render("Bağlı izleyici"), stText.Bold(true).Render(fmt.Sprintf("%d kişi  ", a.party.PeerCount())), iw),
			"",
			stFaint.Render(strings.Repeat("─", iw)),
			"",
		)
		for _, l := range wrap("Arkadaşın aynı bölümü açtığında duraklatma, devam ettirme ve ileri/geri sarma işlemleri anlık olarak eşitlenir (1,5 sn kayma toleransı).", iw-4) {
			body = append(body, "  "+stSub.Render(l))
		}
		body = append(body,
			"",
			center(keycap("c")+" "+stText.Render("adresi kopyala")+"   "+keycap("x")+" "+fg(cErr).Render("odadan çık")+"   "+keycap("esc")+" "+stMuted.Render("oynatmaya dön"), iw),
		)
	} else if p.joining {
		body = append(body,
			center(pill("◈ ODAYA KATIL", cAccent2), iw),
			"",
			"  "+stMuted.Render("Oda adresi (IP:Port):"),
		)
		for _, l := range p.addrIn.view(iw-4, p.focus == 0, a.frame) {
			body = append(body, "  "+l)
		}
		body = append(body, "", "  "+stMuted.Render("Takma adın:"))
		for _, l := range p.nickIn.view(iw-4, p.focus == 1, a.frame) {
			body = append(body, "  "+l)
		}
		body = append(body, "", center(keycap("enter")+" "+fg(cOK).Bold(true).Render("Bağlan")+"   "+keycap("tab")+" "+stMuted.Render("alan")+"   "+keycap("esc")+" "+stMuted.Render("iptal"), iw))
	} else {
		body = append(body,
			center(pill("◈ BİRLİKTE İZLE (WATCH PARTY)", cAccent), iw),
			"",
			center(gradient("Arkadaşlarınla Eşzamanlı İzle", brand, true), iw),
			"",
		)
		for _, l := range wrap("Ek sunucu veya hesap gerekmez. Biriniz oda kurar, diğerleri IP:Port (yerel ağ, Tailscale veya port yönlendirme) ile bağlanır; duraklatma ve sarma her iki mpv penceresinde senkron kalır.", iw-4) {
			body = append(body, "  "+stSub.Render(l))
		}
		body = append(body,
			"",
			"  "+keycap("h")+"  "+stText.Bold(true).Render("Oda kur")+stMuted.Render("   — yerel port açar ve paylaşılacak adresi kopyalar"),
			"  "+keycap("j")+"  "+stText.Bold(true).Render("Odaya katıl")+stMuted.Render(" — arkadaşının paylaştığı adrese bağlanır"),
			"",
		)
		if watchparty.HasSyncplay() {
			body = append(body, "  "+fg(cOK).Render("✓ ")+stMuted.Render("Sistemde Syncplay de algılandı."))
		}
	}

	innerLines := append([]string{""}, body...)
	innerLines = append(innerLines, "")
	panelH := min(h-2, max(12, len(innerLines)+2))
	boxed := panel("birlikte izle", innerLines, cw, panelH, true, cAccent)

	top := max(0, (h-len(boxed))/2)
	out := make([]string, 0, h)
	for i := 0; i < top; i++ {
		out = append(out, "")
	}
	for _, l := range boxed {
		out = append(out, center(l, w))
	}
	return strings.Join(block(out, w, h), "\n")
}
