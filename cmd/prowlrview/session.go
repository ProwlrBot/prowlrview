package main

import (
	"fmt"
	"os"

	"github.com/ProwlrBot/prowlrview/internal/session"
)

func runSession(args []string) {
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "new", "create":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "session new: missing NAME")
			os.Exit(2)
		}
		name := args[1]
		target := ""
		if len(args) > 2 {
			target = args[2]
		}
		s, err := session.New(name, target)
		if err != nil {
			die(err)
		}
		if err := session.Switch(name); err != nil {
			die(err)
		}
		fmt.Printf("✓ session %q created", s.Name)
		if s.Target != "" {
			fmt.Printf(" (target: %s)", s.Target)
		}
		fmt.Println()
		fmt.Printf("  snapshot: %s\n", session.SnapshotPath(name))

	case "list", "ls":
		sessions, err := session.List()
		if err != nil {
			die(err)
		}
		active := session.Active()
		if len(sessions) == 0 {
			fmt.Println("  no sessions yet — run: prowlrview session new <name> [target]")
			return
		}
		for _, s := range sessions {
			flag := " "
			if s.Name == active {
				flag = "▶"
			}
			tgt := s.Target
			if tgt == "" {
				tgt = "(no target)"
			}
			fmt.Printf("  %s %-20s %s  updated %s\n", flag, s.Name, tgt,
				s.UpdatedAt.Format("2006-01-02 15:04"))
		}

	case "switch", "use":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "session switch: missing NAME")
			os.Exit(2)
		}
		if err := session.Switch(args[1]); err != nil {
			die(err)
		}
		fmt.Printf("✓ switched to session %q\n", args[1])

	case "active", "current":
		name := session.Active()
		if name == "" {
			fmt.Println("(no active session)")
		} else {
			fmt.Println(name)
		}

	case "delete", "rm":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "session delete: missing NAME")
			os.Exit(2)
		}
		name := args[1]
		dir := session.SnapshotPath(name)
		_ = dir // prevent unused warning
		// remove session dir
		home, _ := os.UserHomeDir()
		sdir := home + "/.local/share/prowlrview/sessions/" + name
		if err := os.RemoveAll(sdir); err != nil {
			die(err)
		}
		fmt.Printf("✓ deleted session %q\n", name)

	default:
		fmt.Fprintln(os.Stderr, "session: unknown subcommand:", args[0])
		os.Exit(2)
	}
}
