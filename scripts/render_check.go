package main

import (
	"bytes"
	"fmt"

	"github.com/maelmoreau21/JellyGate/internal/render"
)

func main() {
	engine, err := render.NewEngine("web/templates", "web/i18n")
	if err != nil {
		fmt.Printf("engine init error: %v\n", err)
		return
	}
	td := engine.NewTemplateData("fr")
	td.ScriptNonce = "nonce"
	td.AdminUsername = "tester"
	td.IsAdmin = true
	td.CanInvite = true

	var buf bytes.Buffer
	if err := engine.Render(&buf, "admin/login.html", td); err != nil {
		fmt.Printf("render login error: %v\n", err)
	} else {
		s := buf.String()
		fmt.Printf("login rendered OK, len=%d\n", len(s))
		if len(s) > 200 {
			fmt.Printf("login head: %q\n", s[:200])
		}
	}

	buf.Reset()
	if err := engine.Render(&buf, "admin/dashboard.html", td); err != nil {
		fmt.Printf("render dashboard error: %v\n", err)
	} else {
		s := buf.String()
		fmt.Printf("dashboard rendered OK, len=%d\n", len(s))
		if len(s) > 200 {
			fmt.Printf("dashboard head: %q\n", s[:200])
		}
	}
}
