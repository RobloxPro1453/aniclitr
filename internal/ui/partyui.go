package ui

import (
	"context"
	"os/user"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/nyaa"
	"aniclitr/internal/watchparty"
)

// partyScreen manages Watch Party 2.0: 100% serverless internet rooms by room
// code (zero port forwarding, zero local server) + live mpv ASS chat & emoji
// reactions (toggleable via Alt+H).
type partyScreen struct {
	joining  bool // entering a room code
	chatting bool // typing a live chat message
	codeIn   input
	nickIn   input
	chatIn   input
	focus    int // 0 code, 1 nick
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
		codeIn: input{placeholder: "oda kodu (ör. ANI-7K4M-9P2Q)"},
		nickIn: input{placeholder: "görünen ad"},
		chatIn: input{placeholder: "canlı sohbet mesajı yazın…"},
	}
	p.nickIn.Set(defaultNick())
	return p
}

func (p *partyScreen) crumb() string { return "birlikte izle" }

func (p *partyScreen) typing() bool { return p.joining || p.chatting }

func (p *partyScreen) hints() []hint {
	if p.joining {
		return []hint{{"enter", "odaya katıl"}, {"tab", "alan değiştir"}, {"esc", "vazgeç"}}
	}
	if p.chatting {
		return []hint{{"enter", "gönder"}, {"esc", "vazgeç"}}
	}
	return []hint{{"h", "oda kodu oluştur"}, {"j", "koda katıl"}, {"t", "sohbet yaz"}, {"1-5", "tepki"}, {"c", "kodu kopyala"}, {"x", "çık"}, {"esc", "geri"}}
}

var partyReactions = []string{"🔥", "😭", "😂", "👏", "⚡"}

func (p *partyScreen) update(a *App, msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	if p.chatting {
		switch k.String() {
		case "esc":
			p.chatting = false
			return nil
		case "enter":
			txt := strings.TrimSpace(p.chatIn.Value())
			p.chatIn.Set("")
			p.chatting = false
			if txt != "" && a.relayParty != nil {
				if err := a.relayParty.SendChat(txt); err != nil {
					a.notify("mesaj gönderilemedi: "+err.Error(), "err")
				} else {
					a.notify("💬 gönderildi", "ok")
				}
				return a.tick()
			}
			return nil
		default:
			p.chatIn.handle(k)
			return nil
		}
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
			code := strings.TrimSpace(p.codeIn.Value())
			nick := strings.TrimSpace(p.nickIn.Value())
			if code == "" {
				a.notify("oda kodu girin (ör. ANI-7K4M-9P2Q)", "warn")
				return a.tick()
			}
			if a.relayParty != nil {
				a.relayParty.Close()
			}
			rr, err := watchparty.ConnectRelay(context.Background(), "", code, nick, nil)
			if err != nil {
				a.notify(err.Error(), "err")
				return a.tick()
			}
			a.relayParty = rr
			p.joining = false
			a.notify("◈ odaya bağlanıldı: "+rr.Code(), "ok")
			return tea.Batch(a.waitRelayParty(), a.tick())
		default:
			if p.focus == 0 {
				p.codeIn.handle(k)
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
		if a.relayParty != nil {
			a.relayParty.Close()
		}
		code := watchparty.GenerateRoomCode()
		rr, err := watchparty.ConnectRelay(context.Background(), "", code, defaultNick(), nil)
		if err != nil {
			a.notify(err.Error(), "err")
			return a.tick()
		}
		a.relayParty = rr
		_ = nyaa.CopyClipboard(rr.Code())
		a.notify("◈ oda kodu oluşturuldu ve kopyalandı: "+rr.Code(), "ok")
		return tea.Batch(a.waitRelayParty(), a.tick())
	case "j", "J":
		p.joining = true
		p.focus = 0
		return a.tick()
	case "t", "T":
		if a.relayParty != nil {
			p.chatting = true
			return a.tick()
		}
	case "1", "2", "3", "4", "5":
		if a.relayParty != nil {
			idx := int(k.String()[0] - '1')
			if idx >= 0 && idx < len(partyReactions) {
				_ = a.relayParty.SendReaction(partyReactions[idx])
				a.notify("tepki gönderildi: "+partyReactions[idx], "ok")
				return a.tick()
			}
		}
	case "c", "C":
		if a.relayParty != nil {
			if err := nyaa.CopyClipboard(a.relayParty.Code()); err != nil {
				a.notify("kopyalanamadı: "+err.Error(), "warn")
			} else {
				a.notify("oda kodu kopyalandı: "+a.relayParty.Code(), "ok")
			}
			return a.tick()
		}
	case "x", "X":
		if a.relayParty != nil {
			a.relayParty.Close()
			a.relayParty = nil
			a.notify("birlikte izleme odasından çıkıldı", "ok")
			return a.tick()
		}
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
	cw := clamp(w-8, 54, 86)
	iw := cw - 4
	var body []string

	if a.relayParty != nil {
		body = append(body,
			center(pill("◈ BİRLİKTE İZLE · AKTİF ODA", cOK), iw),
			"",
			row2("  "+stMuted.Render("Oda Kodu"), fg(cAccent).Bold(true).Render(a.relayParty.Code()+"  "), iw),
			row2("  "+stMuted.Render("Oynatıcı Sohbet Katmanı"), stText.Render(map[bool]string{true: "açık (Alt+H gizle)  ", false: "gizli (Alt+H aç)  "}[a.chatVisible]), iw),
			"",
			stFaint.Render(strings.Repeat("─", iw)),
		)
		msgs := a.relayParty.Messages()
		if len(msgs) == 0 {
			body = append(body, "", "  "+stMuted.Render("Henüz mesaj yok. Sohbet için t, tepki için 1-5 tuşlarını kullanın."))
		} else {
			start := max(0, len(msgs)-5)
			for _, m := range msgs[start:] {
				txt := strings.TrimSpace(m.Reaction + " " + m.Text)
				body = append(body, "  "+fg(cSky).Bold(true).Render(m.Nick+": ")+stText.Render(truncate(txt, iw-18)))
			}
		}
		if p.chatting {
			body = append(body, "")
			for _, l := range p.chatIn.view(iw-4, true, a.frame) {
				body = append(body, "  "+l)
			}
		}
		body = append(body,
			"",
			center(keycap("t")+" "+stText.Render("sohbet")+"  "+keycap("1-5")+" "+stText.Render("🔥😭😂👏⚡")+"  "+keycap("c")+" "+stText.Render("kopyala")+"  "+keycap("x")+" "+fg(cErr).Render("çık"), iw),
		)
	} else if p.joining {
		body = append(body,
			center(pill("◈ ODAYA KATIL", cAccent2), iw),
			"",
			"  "+stMuted.Render("Oda Kodu:"),
		)
		for _, l := range p.codeIn.view(iw-4, p.focus == 0, a.frame) {
			body = append(body, "  "+l)
		}
		body = append(body, "", "  "+stMuted.Render("Görünen ad:"))
		for _, l := range p.nickIn.view(iw-4, p.focus == 1, a.frame) {
			body = append(body, "  "+l)
		}
		body = append(body, "", center(keycap("enter")+" "+fg(cOK).Bold(true).Render("Bağlan")+"   "+keycap("tab")+" "+stMuted.Render("alan")+"   "+keycap("esc")+" "+stMuted.Render("iptal"), iw))
	} else {
		body = append(body,
			center(pill("◈ BİRLİKTE İZLE", cAccent), iw),
			"",
			center(gradient("Oda Koduyla Eşzamanlı İzleme · Canlı Sohbet", brand, true), iw),
			"",
		)
		for _, l := range wrap("Oda kodu oluşturup paylaşın; duraklatma, sarma ve canlı sohbet mesajları (Alt+H) tüm katılımcıların oynatıcılarında eşzamanlı olarak çalışır.", iw-4) {
			body = append(body, "  "+stSub.Render(l))
		}
		body = append(body,
			"",
			"  "+keycap("h")+"  "+stText.Bold(true).Render("Oda kodu oluştur")+stMuted.Render(" — yeni oda kodu üretir ve panoya kopyalar"),
			"  "+keycap("j")+"  "+stText.Bold(true).Render("Odaya katıl")+stMuted.Render(" — paylaşılan oda koduna bağlanır"),
			"",
		)
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
