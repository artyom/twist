// Package twist provides functions to work with Twist API.
//
// It relies on Twist API v3 documented at https://developer.twist.com/v3/
package twist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Client is a Twist API client.
type Client struct {
	token string
}

// New returns Client that calls Twist API using provided token for
// authentication.
//
// See https://developer.twist.com/v3/#authentication for details.
func New(token string) *Client { return &Client{token: token} }

// Workspaces returns all the workspaces user has access to.
func (c *Client) Workspaces(ctx context.Context) ([]Workspace, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.twist.com/api/v3/workspaces/get", nil)
	if err != nil {
		return nil, err
	}
	setAuthHeader(req, c.token)
	body, err := doRequestWithRetries(req)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	var out []Workspace
	if err := json.NewDecoder(body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// Channels returns all the channels in a given workspace.
func (c *Client) Channels(ctx context.Context, workspaceID uint64) ([]Channel, error) {
	if workspaceID == 0 {
		return nil, errors.New("invalid workspace id")
	}
	vals := make(url.Values)
	vals.Add("workspace_id", strconv.FormatUint(workspaceID, 10))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.twist.com/api/v3/channels/get"+"?"+vals.Encode(), nil)
	if err != nil {
		return nil, err
	}
	setAuthHeader(req, c.token)
	body, err := doRequestWithRetries(req)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	var out []Channel
	if err := json.NewDecoder(body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// Workspace is a Twist workspace. A workspace is a shared place between
// different users. Workspace contains channels.
//
// See https://developer.twist.com/v3/#workspaces for details.
type Workspace struct {
	Id   uint64 `json:"id"`
	Name string `json:"name"`
}

// Channel is a Twist channel. Channels organize threads around broad topics
// like team, project, location, or area of interest. Channel contains threads.
//
// See https://developer.twist.com/v3/#channels for details.
type Channel struct {
	Id       uint64 `json:"id"`
	Name     string `json:"name"`
	Archived bool   `json:"archived"`
}

// Thread is a Twist thread. Threads keep team's conversations organized by
// specific topics. Thread contains comments.
//
// See https://developer.twist.com/v3/#threads for details.
type Thread struct {
	Id        uint64 `json:"id"`
	TsPosted  uint64 `json:"posted_ts"`
	TsUpdated uint64 `json:"last_updated_ts"`
	Title     string `json:"title"`
	Text      string `json:"content"`
	Creator   uint64 `json:"creator"`
	// CommentCount int    `json:"comment_count"` // for some reason this is always 0
	Archived bool `json:"is_archived"`
}

// Comment is a message posted to a thread.
//
// See https://developer.twist.com/v3/#comments for details.
type Comment struct {
	Id         uint64 `json:"id"`
	Text       string `json:"content"`
	Creator    uint64 `json:"creator"`
	OrderIndex int    `json:"obj_index"`
	PostedAt   uint64 `json:"posted_ts"`
}

// ThreadsPaginator returns ThreadsPaginator for a channel.
func (c *Client) ThreadsPaginator(channelID uint64) *ThreadsPaginator {
	return &ThreadsPaginator{c: c, channelID: channelID}
}

// ThreadsPaginator fetches all threads in a channel.
//
// Typical usage:
//
//	p := client.ThreadsPaginator(1234) // get threads for channel with id=1234
//	for p.Next() {
//		threads, err := p.Page(ctx)
//		if err != nil {
//			return err
//		}
//		doSomethingWithThreads(threads)
//	}
type ThreadsPaginator struct {
	c         *Client
	channelID uint64
	afterID   uint64
	done      bool
}

// Next reports whether there's another page to load. It only returns false
// once all threads are fetched with the Page method.
func (cp *ThreadsPaginator) Next() bool { return !cp.done }

// Page returns next portion of channel threads.
func (cp *ThreadsPaginator) Page(ctx context.Context) ([]Thread, error) {
	if cp.done {
		return nil, errors.New("all pages already read")
	}
	threads, err := cp.c.getChannelThreadsPage(ctx, cp.channelID, cp.afterID)
	if err != nil {
		return nil, err
	}
	cp.done = len(threads) < maxThreadsPerPage
	if l := len(threads); l != 0 {
		cp.afterID = threads[l-1].Id
	}
	return threads, nil
}

func (c *Client) getChannelThreadsPage(ctx context.Context, channelID, afterID uint64) ([]Thread, error) {
	if channelID == 0 {
		return nil, errors.New("invalid channel ID")
	}
	vals := make(url.Values)
	vals.Add("channel_id", strconv.FormatUint(channelID, 10))
	vals.Add("limit", strconv.Itoa(maxThreadsPerPage))
	vals.Add("order_by", "asc")
	if afterID == 0 {
		// Twist has (had?) an issue — it ignored after_id=0, resulting in
		// response sorted by update timestamp instead of id
		vals.Add("after_id", "-1")
	} else {
		vals.Add("after_id", strconv.FormatUint(afterID, 10))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.twist.com/api/v3/threads/get", strings.NewReader(vals.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	setAuthHeader(req, c.token)
	body, err := doRequestWithRetries(req)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var out []Thread
	if err := json.NewDecoder(body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	if !sort.SliceIsSorted(out, func(i, j int) bool { return out[i].Id < out[j].Id }) {
		// for _, v := range out {
		// 	log.Println("id:", v.Id, "last_updated:", v.TsUpdated)
		// }
		return nil, errors.New("API returned threads that are not properly sorted by ids")
	}
	return out, nil
}

// CommentsPaginator returns CommentsPaginator for a thread.
func (c *Client) CommentsPaginator(threadID uint64) *CommentsPaginator {
	return &CommentsPaginator{c: c, threadID: threadID}
}

// CommentsPaginator fetches all comments in a thread.
//
// Typical usage:
//
// 	p := client.CommentsPaginator(3456) // get comments for thread with id=3456
// 	for p.Next() {
// 		comments, err := p.Page(ctx)
// 		if err != nil {
// 			return err
// 		}
// 		doSomethingWithComments(comments)
// 	}
type CommentsPaginator struct {
	c         *Client
	threadID  uint64
	nextIndex int
	done      bool
}

// Next reports whether there's another page to load. It only returns false
// once all channels are fetched with the Page method.
func (tp *CommentsPaginator) Next() bool { return !tp.done }

// Page returns next portion of thread comments.
func (tp *CommentsPaginator) Page(ctx context.Context) ([]Comment, error) {
	if tp.done {
		return nil, errors.New("all pages already read")
	}
	comments, err := tp.c.getThreadCommentsPage(ctx, tp.threadID, tp.nextIndex)
	if err != nil {
		return nil, err
	}
	tp.done = len(comments) < maxCommentsPerPage
	if l := len(comments); l != 0 {
		tp.nextIndex = comments[l-1].OrderIndex + 1
	}
	return comments, nil
}

func (c *Client) getThreadCommentsPage(ctx context.Context, threadID uint64, fromIndex int) ([]Comment, error) {
	if fromIndex < 0 {
		panic("fromIndex must be non-negative")
	}
	if threadID == 0 {
		return nil, errors.New("invalid thread ID")
	}
	vals := make(url.Values)
	vals.Add("thread_id", strconv.FormatUint(threadID, 10))
	vals.Add("limit", strconv.Itoa(maxCommentsPerPage))
	vals.Add("from_obj_index", strconv.Itoa(fromIndex))
	// API returns results including both {from,to}_obj_index, it calculates
	// result like [from_obj_index, to_obj_index][:limit]
	vals.Add("to_obj_index", strconv.Itoa(fromIndex+maxCommentsPerPage-1))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.twist.com/api/v3/comments/get"+"?"+vals.Encode(), nil)
	if err != nil {
		return nil, err
	}
	setAuthHeader(req, c.token)
	body, err := doRequestWithRetries(req)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var out []Comment
	if err := json.NewDecoder(body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	if !sort.SliceIsSorted(out, func(i, j int) bool { return out[i].OrderIndex < out[j].OrderIndex }) {
		return nil, errors.New("API returned comments that are not properly sorted by obj_index")
	}
	var offset int
	for i, c := range out { // sanity check
		if i == 0 {
			offset = c.OrderIndex
			continue
		}
		if want := offset + i; c.OrderIndex != want {
			return nil, fmt.Errorf("ordering issue in comments: order index is %d, expected %d",
				c.OrderIndex, want)
		}
	}
	return out, nil
}

// doRequestWithRetries calls http.DefaultClient.Do for a given request. It
// checks that response is 200 OK, and has an "application/json" Content-Type.
// If response code is 429 Too Many Requests, or one of 5xx, function
// automatically retries request up to a limited number of attempts. It returns
// response body on success.
func doRequestWithRetries(req *http.Request) (io.ReadCloser, error) {
	attempt := func(req *http.Request) (body io.ReadCloser, tryAgain bool, err error) {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, false, err
		}
		var defuseBodyClose bool
		defer func() {
			if defuseBodyClose {
				return
			}
			resp.Body.Close()
		}()
		switch {
		case resp.StatusCode == http.StatusOK:
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError:
			return nil, true, fmt.Errorf("unexpected status: %q", resp.Status)
		default:
			return nil, false, fmt.Errorf("unexpected status: %q", resp.Status)
		}
		if ct := resp.Header.Get(headerContentType); ct != jsonContentType {
			return nil, false, fmt.Errorf("unexpected Content-Type: %q", ct)
		}
		defuseBodyClose = true
		return resp.Body, false, nil
	}

	var ticker *time.Ticker
	const maxRetries = 10
	var lastError error

	for n := 0; n < maxRetries; n++ {
		if n != 0 && req.Body != nil {
			if seeker, ok := req.Body.(io.Seeker); ok {
				if _, err := seeker.Seek(0, io.SeekStart); err != nil {
					return nil, fmt.Errorf("rewinding request body to 0: %w", err)
				}
			} else {
				return nil, fmt.Errorf("cannot rewind non-nil request body for retry, last error was %w", lastError)
			}
		}
		if n != 0 {
			if ticker == nil {
				ticker = time.NewTicker(500 * time.Millisecond)
				defer ticker.Stop()
			}
			select {
			case <-ticker.C:
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}
		body, tryAgain, err := attempt(req)
		if err != nil {
			lastError = err
			if tryAgain {
				continue
			}
			return nil, err
		}
		return body, nil
	}
	return nil, fmt.Errorf("giving up after %d retries, last error was %w", maxRetries, lastError)
}

func setAuthHeader(r *http.Request, token string) {
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("User-Agent", "github.com/artyom/twist")
}

const jsonContentType = "application/json"
const headerContentType = "Content-Type"

const maxThreadsPerPage = 100
const maxCommentsPerPage = 500
