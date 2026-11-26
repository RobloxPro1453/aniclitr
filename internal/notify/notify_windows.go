//go:build windows

package notify

import (
	"fmt"
	"os/exec"
)

func sendOS(title, body string) {
	// Uses built-in Windows 10/11 WinRT ToastNotificationManager with zero dependencies.
	script := fmt.Sprintf(`
$ErrorActionPreference = 'SilentlyContinue'
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null
$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml('<toast><visual><binding template="ToastText02"><text id="1">%s</text><text id="2">%s</text></binding></visual></toast>')
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('aniclitr').Show($toast)
`, xmlEscape(title), xmlEscape(body))
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	_ = cmd.Run()
}
