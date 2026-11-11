package main

import (
	"log"
	"strings"
)

type ActionKind int

const (
	ActionOpen       ActionKind = iota // open a file or web link
	ActionShowFolder                   // show a file in the file manager
	ActionCopy                         // copy text to the clipboard
)

type Action struct {
	Label  string
	Kind   ActionKind
	Target string
}

// Notice is one desktop notification.
type Notice struct {
	Title   string
	Body    string
	Image   string // optional picture to show (a received photo)
	Actions []Action
	Default *Action // what clicking the notification itself does
	// Tag, when set, makes this notice replace an earlier one with the same tag
	// ("Receiving video.mov…" → "Video from Hiha").
	Tag string
}

type Notifier interface {
	Notify(Notice)
}

// SystemNotifier shows notifications with the platform's own mechanism (platform_*.go).
type SystemNotifier struct{}

// LogNotifier only logs: for tests and headless machines.
type LogNotifier struct{}

func (LogNotifier) Notify(n Notice) { log.Printf("notice: %s: %s", n.Title, n.Body) }

// run performs an action chosen in a notification.
func run(a Action) {
	var err error
	switch a.Kind {
	case ActionOpen:
		if strings.HasPrefix(a.Target, "http://") || strings.HasPrefix(a.Target, "https://") {
			err = OpenURL(a.Target)
		} else {
			err = OpenPath(a.Target)
		}
	case ActionShowFolder:
		err = ShowInFolder(a.Target)
	case ActionCopy:
		err = CopyText(a.Target)
	}
	if err != nil {
		log.Printf("%s: %v", a.Label, err)
	}
}
