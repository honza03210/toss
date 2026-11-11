package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// cmdSend queues files, text or the clipboard for the phone (it picks them up when Toss opens).
//
//	toss send <file>…        toss send --text "hello"     toss send --clipboard
//	echo hi | toss send -    toss send https://example.com
//
// --notify reports with a desktop notification instead of printing (the file manager's
// "Send to iPhone" runs it without a terminal).
func cmdSend(args []string) error {
	store, err := OpenStore()
	if err != nil {
		return err
	}
	notify := false
	var texts, files []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--notify":
			notify = true
		case a == "--text" || a == "-t":
			if i+1 >= len(args) {
				return errors.New("--text needs the text to send")
			}
			i++
			texts = append(texts, args[i])
		case a == "--clipboard" || a == "-c":
			text, err := PasteText()
			if err != nil {
				return err
			}
			if strings.TrimSpace(text) == "" {
				return errors.New("the clipboard has no text")
			}
			texts = append(texts, text)
		case a == "-":
			data, err := io.ReadAll(io.LimitReader(os.Stdin, maxTextBytes+1))
			if err != nil {
				return err
			}
			texts = append(texts, string(data))
		case a == "--":
			files = append(files, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(a, "-") && len(a) > 1:
			return fmt.Errorf("unknown option %s", a)
		default:
			if _, err := os.Stat(a); err != nil && isWebLink(a) {
				texts = append(texts, a)
			} else {
				files = append(files, a)
			}
		}
	}
	if len(texts)+len(files) == 0 {
		return errors.New("usage: toss send <file>… | --text <text> | --clipboard | - (stdin)")
	}
	var labels []string
	var failed []string
	for _, t := range texts {
		it, err := store.AddOutText(t)
		if err != nil {
			failed = append(failed, err.Error())
			continue
		}
		labels = append(labels, it.Label())
	}
	for _, f := range files {
		it, err := store.AddOutFile(f)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", filepath.Base(f), cleanErr(err)))
			continue
		}
		labels = append(labels, it.Label())
	}
	phone := "the iPhone"
	if devs := store.Devices(); len(devs) == 1 {
		phone = devs[0].Name
	} else if len(devs) == 0 {
		failed = append(failed, "no phone is paired yet (toss pair)")
	}
	summary := ""
	switch len(labels) {
	case 0:
	case 1:
		summary = fmt.Sprintf("%s is ready for %s", labels[0], phone)
	default:
		summary = fmt.Sprintf("%d items are ready for %s", len(labels), phone)
	}
	hint := "Open Toss on " + phone + " to get it."
	if notify {
		n := Notice{Title: summary, Body: hint}
		if len(failed) > 0 {
			n.Body = "Couldn't send: " + strings.Join(failed, "; ")
			if summary == "" {
				n.Title = "Nothing sent to " + phone
			}
		}
		SystemNotifier{}.Notify(n)
		// Give notify-send/PowerShell a moment before this short-lived process exits.
		time.Sleep(700 * time.Millisecond)
	} else {
		if summary != "" {
			fmt.Printf("✓ %s. %s\n", summary, hint)
		}
		for _, f := range failed {
			fmt.Fprintln(os.Stderr, "✗", f)
		}
	}
	if len(labels) == 0 {
		return errors.New("nothing was sent")
	}
	return nil
}

func cleanErr(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// cmdInbox lists what waits for the phone; `toss inbox clear` drops it all.
func cmdInbox(args []string) error {
	store, err := OpenStore()
	if err != nil {
		return err
	}
	items := store.Outbox()
	if len(args) == 1 && args[0] == "clear" {
		for _, it := range items {
			store.RemoveOut(it.ID)
		}
		fmt.Printf("✓ Removed %d waiting items.\n", len(items))
		return nil
	}
	if len(items) == 0 {
		fmt.Println("Nothing is waiting for the phone. Send something with `toss send <file>`.")
		return nil
	}
	fmt.Println("Waiting for the phone (it picks them up when Toss opens):")
	for _, it := range items {
		extra := ""
		if it.Kind == "file" {
			extra = " (" + humanSize(it.Size) + ")"
		}
		fmt.Printf("  %s  %s%s\n", it.Created.Format("Jan 2 15:04"), it.Label(), extra)
	}
	return nil
}

// cmdHistory prints what was received, newest first.
func cmdHistory(args []string) error {
	store, err := OpenStore()
	if err != nil {
		return err
	}
	if len(args) == 1 && args[0] == "clear" {
		if err := store.ClearHistory(); err != nil {
			return err
		}
		fmt.Println("✓ History cleared (received files stay where they are).")
		return nil
	}
	list := store.History()
	if len(list) == 0 {
		fmt.Println("Nothing received yet.")
		return nil
	}
	limit := 20
	if len(args) == 1 {
		if _, err := fmt.Sscan(args[0], &limit); err != nil || limit < 1 {
			return errors.New("usage: toss history [count | clear]")
		}
	}
	if len(list) > limit {
		list = list[:limit]
	}
	for _, e := range list {
		what := e.Path
		if e.Kind != "file" {
			what = truncate(strings.ReplaceAll(e.Text, "\n", " "), 90)
		}
		fmt.Printf("%s  %-5s %s  (%s)\n", e.Time.Format("Jan 2 15:04"), e.Kind, what, e.Device)
	}
	return nil
}

// cmdWeb opens the local page in the browser.
func cmdWeb(args []string) error {
	store, err := OpenStore()
	if err != nil {
		return err
	}
	cfg, err := store.Config()
	if err != nil {
		return err
	}
	if !running(cfg.Port) {
		return errors.New("Toss isn't running: start it with `toss install` (or `toss serve`)")
	}
	token, err := store.WebToken()
	if err != nil {
		return err
	}
	link := WebURL(cfg, token)
	if len(args) == 1 && args[0] == "--pair" {
		link = strings.Replace(link, "/#", "/?pair#", 1)
	}
	if err := OpenURL(link); err != nil {
		return err
	}
	fmt.Printf("Opened the Toss page in your browser (http://127.0.0.1:%d).\n", webPort(cfg))
	return nil
}
