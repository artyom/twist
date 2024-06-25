package main

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"

	"github.com/artyom/twist"
)

func main() {
	log.SetFlags(0)
	flag.Parse()
	if err := run(context.Background(), flag.Arg(0)); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, threadUrl string) error {
	if threadUrl == "" {
		return errors.New("want Twist thread url as the first argument")
	}
	token := os.Getenv("TWIST_TOKEN")
	if token == "" {
		return errors.New("please set TWIST_TOKEN env")
	}
	ids, err := tidFromUrl(threadUrl)
	if err != nil {
		return err
	}
	client := twist.New(token)
	users, err := client.Users(ctx, ids.workspace)
	if err != nil {
		return fmt.Errorf("getting workspace users: %w", err)
	}
	uidToName := make(map[uint64]string)
	for _, u := range users {
		uidToName[u.Id] = cmp.Or(u.ShortName, u.Name)
	}
	thread, err := client.Thread(ctx, ids.thread)
	if err != nil {
		return fmt.Errorf("reading thread: %w", err)
	}
	var buf bytes.Buffer
	buf.WriteString("<post>\n")
	fmt.Fprintf(&buf, "<author>%s</author>\n", cmp.Or(uidToName[thread.Creator], "UNKNOWN USER"))
	fmt.Fprintf(&buf, "# %s\n\n", thread.Title)
	fmt.Fprintln(&buf, clearMentions(thread.Text))
	buf.WriteString("</post>\n")
	p := client.CommentsPaginator(ids.thread)
	for p.Next() {
		comments, err := p.Page(ctx)
		if err != nil {
			return fmt.Errorf("reading thread comments: %w", err)
		}
		for _, c := range comments {
			buf.WriteString("<comment>\n")
			fmt.Fprintf(&buf, "<author>%s</author>\n", cmp.Or(uidToName[c.Creator], "UNKNOWN USER"))
			fmt.Fprintln(&buf, clearMentions(c.Text))
			buf.WriteString("</comment>\n")
		}
	}
	_, err = os.Stdout.Write(buf.Bytes())
	return err
}

var twistThreadUrl = regexp.MustCompile(`^https://twist\.com/a/(\d+)/ch/(\d+)/t/(\d+)/?$`)

func tidFromUrl(url string) (*tid, error) {
	m := twistThreadUrl.FindStringSubmatch(url)
	if m == nil {
		return nil, fmt.Errorf("%q does not match %v", url, twistThreadUrl)
	}
	var out tid
	var err error
	if out.workspace, err = strconv.ParseUint(m[1], 10, 64); err != nil {
		return nil, err
	}
	if out.channel, err = strconv.ParseUint(m[2], 10, 64); err != nil {
		return nil, err
	}
	if out.thread, err = strconv.ParseUint(m[3], 10, 64); err != nil {
		return nil, err
	}
	return &out, nil
}

type tid struct {
	workspace, channel, thread uint64
}

var mentionRe = regexp.MustCompile(`\[(?<name>[^\]]+)\]\(twist-mention://\d+\)`)

func clearMentions(text string) string { return mentionRe.ReplaceAllString(text, "${name}") }
